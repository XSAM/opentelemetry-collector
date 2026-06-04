// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configcredentials // import "go.opentelemetry.io/collector/config/configcredentials"

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
)

var (
	errMultipleAuthTypes = errors.New("authentication: exactly one auth type may be configured")
	errUnknownAuthType   = errors.New("authentication: no provider factory supplied for auth type")
)

// Authentication is the embeddable credentials config a connection-oriented
// component squashes into its own config. Exactly one auth-type key is set, and
// its value is that auth type's sub-config:
//
//	authentication:
//	  aws_iam:
//	    region: ap-northeast-2
//
// The auth-type key (here "aws_iam") selects a ProviderFactory from the explicit
// set the consumer supplies to Resolve. Resolve unmarshals the sub-config into
// the chosen factory's config and builds the Provider.
type Authentication struct {
	// Settings holds each configured auth type keyed by its name. The ",remain"
	// tag captures every key in the authentication block; exactly one is expected.
	// It is exported because mapstructure cannot populate unexported fields; treat
	// it as read-only and prefer the IsEmpty/Validate/Resolve methods.
	Settings map[string]any `mapstructure:",remain"`
}

// IsEmpty reports whether no auth type is configured. A component treats an empty
// Authentication as "credentials not in use" and falls back to its existing
// static credential fields — the framework is opt-in.
func (a Authentication) IsEmpty() bool {
	return len(a.Settings) == 0
}

// Validate fails when more than one auth type is configured. Zero is allowed
// (opt-out); the unknown-type case is reported by Resolve, which is the only
// place the factory set is known.
func (a Authentication) Validate() error {
	if len(a.Settings) > 1 {
		return fmt.Errorf("%w, got %d", errMultipleAuthTypes, len(a.Settings))
	}
	return nil
}

// Resolve selects the configured auth type's factory from the supplied set,
// unmarshals the sub-config into the factory's config, and builds the Provider.
// It returns (nil, nil) when no auth type is configured (opt-out). It returns an
// error when more than one auth type is set, when the configured type matches no
// factory in the set, or when the factory set itself is malformed (duplicate or
// invalid types).
func (a Authentication) Resolve(set ProviderSettings, factories []ProviderFactory) (Provider, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if a.IsEmpty() {
		return nil, nil
	}

	fMap, err := newProviderFactoryMap(factories)
	if err != nil {
		return nil, err
	}

	authType, raw := a.single()
	factory, ok := fMap[authType]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnknownAuthType, authType)
	}

	cfg := factory.CreateDefaultConfig()
	if raw != nil {
		if err := confmap.NewFromStringMap(raw).Unmarshal(cfg); err != nil {
			return nil, fmt.Errorf("authentication: failed to unmarshal config for auth type %q: %w", authType, err)
		}
	}

	// authType was validated as a component type by newProviderFactoryMap above
	// (it is a key in fMap), so NewType cannot fail here; handle the error
	// defensively rather than panicking via MustNewID.
	authComponentType, err := component.NewType(authType)
	if err != nil {
		return nil, fmt.Errorf("authentication: invalid auth type %q: %w", authType, err)
	}
	set.ID = component.NewID(authComponentType)
	return factory.CreateProvider(set, cfg)
}

// single returns the sole configured auth type and its sub-config as a string
// map. Callers must ensure exactly one key is set (Validate + non-empty). The
// sub-config is nil when the key has no value (e.g. "aws_iam:" with no body).
func (a Authentication) single() (string, map[string]any) {
	for k, v := range a.Settings {
		sub, _ := v.(map[string]any)
		return k, sub
	}
	return "", nil
}
