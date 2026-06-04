// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package configcredentials implements the configuration settings for
// connection-oriented credentials. Unlike configauth, which supplies
// transport-level authentication for HTTP/gRPC requests, this package supplies a
// credential (username/secret) to a connection-oriented component (such as a
// database receiver) at connection-open time, with support for credentials that
// expire or change out-of-band.
//
// A credential provider is a ProviderFactory selected by an inline auth-type key
// in a component's Authentication config block. Factories are supplied to the
// component as an explicit slice; there is no global registry and no init()-based
// registration.
package configcredentials // import "go.opentelemetry.io/collector/config/configcredentials"

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/collector/component"
)

var (
	errDuplicateFactory   = errors.New("duplicate credentials provider factory type")
	errInvalidFactoryType = errors.New("credentials provider factory has an invalid Type()")
)

// Credential is the fixed value a Provider returns. The same shape is used by
// every auth type: a credential carries optional material plus an optional
// expiry, and nothing about how that material should be applied — the consuming
// component decides that.
type Credential struct {
	// Username is the username the consumer should use. A nil pointer means the
	// provider does not supply a username and the consumer should fall back to its
	// own configured username; a non-nil pointer (including a pointer to the empty
	// string) means the provider generated the username and the consumer must use
	// it. The nil-vs-empty distinction is load-bearing, so this is a pointer.
	Username *string

	// Secret is the opaque secret material — a password or a minted token. It is
	// placed wherever the consuming driver expects the secret (e.g. the password
	// slot of a connection string). It is never logged.
	Secret string

	// NotAfter is an advisory hint for when the credential expires. A nil pointer
	// means no expiry applies (e.g. a static password). It is advisory only: the
	// provider owns refresh-before-expiry, so consumers need not act on it, but it
	// is useful for observability.
	NotAfter *time.Time

	// prevent unkeyed literal initialization
	_ struct{}
}

// Provider supplies a Credential to a connection-oriented component. The provider
// owns any caching and refresh-before-expiry; a consumer may call GetCredential
// on every connection-open and trust the returned credential is currently valid.
type Provider interface {
	// GetCredential returns the credential valid at the time of the call.
	GetCredential(ctx context.Context) (*Credential, error)

	// Watch registers onChange, which the provider invokes when its credential
	// changes out-of-band (for example, a rotated credential file). onChange is
	// invoked with the new credential. Watch returns stop, which the consumer
	// calls to unsubscribe and release any provider-side resources, and an error
	// if the watch could not be established.
	//
	// Providers whose credential never changes out-of-band embed NopWatcher to
	// satisfy this method with a no-op, so consumers can call Watch unconditionally
	// without a capability type assertion.
	Watch(ctx context.Context, onChange func(*Credential)) (stop func(), err error)
}

// NopWatcher is an embeddable no-op implementation of Provider.Watch for
// providers whose credential never changes out-of-band (for example, a provider
// that mints a fresh token on every GetCredential call). It registers
// successfully and never invokes the callback — an accurate description of a
// pull-only source, not a stub that pretends to watch.
type NopWatcher struct{}

// Watch implements the Watch method of Provider with a no-op: the returned stop
// is safe to call and the callback is never invoked.
func (NopWatcher) Watch(context.Context, func(*Credential)) (func(), error) {
	return func() {}, nil
}

// ProviderSettings is passed to a ProviderFactory when it builds a Provider.
type ProviderSettings struct {
	// ID is the configured auth-type identity (its Type as a component.ID).
	ID component.ID

	// TelemetrySettings provides the provider with telemetry APIs (logger, tracer,
	// meter).
	TelemetrySettings component.TelemetrySettings

	// BuildInfo describes the collector build.
	BuildInfo component.BuildInfo

	// prevent unkeyed literal initialization
	_ struct{}
}

// ProviderFactory builds a Provider for one auth type. A factory is identified by
// its Type (the inline config key, e.g. "aws_iam") and is supplied to a consuming
// component as one entry in an explicit []ProviderFactory — there is no global
// registry and no init()-based registration, mirroring confmap.ProviderFactory.
type ProviderFactory interface {
	// Type returns the auth-type key this factory handles, e.g. "aws_iam". It is
	// the key used in the inline Authentication config block.
	Type() string

	// CreateDefaultConfig returns the zero value of this factory's sub-config, into
	// which the inline Authentication block is unmarshaled.
	CreateDefaultConfig() component.Config

	// CreateProvider builds a Provider from the unmarshaled sub-config. Any
	// connection inputs a provider needs (such as a database endpoint or username
	// for token minting) come from the factory's own sub-config.
	CreateProvider(set ProviderSettings, cfg component.Config) (Provider, error)
}

// newProviderFactoryMap builds a Type()-keyed map from an explicit slice of
// factories. It returns an error if two factories share a Type or a factory has
// a Type that is not a valid component type (the charset/length rules enforced
// by component.NewType). Validating here means an invalid Type surfaces as an
// error from the factory set rather than as a panic later in Resolve. There is
// intentionally no global registry: the consuming component owns the factory set.
func newProviderFactoryMap(factories []ProviderFactory) (map[string]ProviderFactory, error) {
	fMap := make(map[string]ProviderFactory, len(factories))
	for _, f := range factories {
		t := f.Type()
		if _, err := component.NewType(t); err != nil {
			return nil, fmt.Errorf("%w: %q: %w", errInvalidFactoryType, t, err)
		}
		if _, ok := fMap[t]; ok {
			return nil, fmt.Errorf("%w: %q", errDuplicateFactory, t)
		}
		fMap[t] = f
	}
	return fMap, nil
}
