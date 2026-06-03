// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configcredentials

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/component"
)

// fakeProvider is a minimal Provider for tests: it embeds NopWatcher (so it
// satisfies Watch with a no-op) and returns a fixed credential.
type fakeProvider struct {
	NopWatcher
	cred *Credential
}

func (f *fakeProvider) GetCredential(context.Context) (*Credential, error) {
	return f.cred, nil
}

// watchingProvider implements Watch for real, to exercise the callback path.
type watchingProvider struct {
	cb      func(*Credential)
	stopped bool
}

func (w *watchingProvider) GetCredential(context.Context) (*Credential, error) {
	return &Credential{Secret: "s"}, nil
}

func (w *watchingProvider) Watch(_ context.Context, onChange func(*Credential)) (func(), error) {
	w.cb = onChange
	return func() { w.stopped = true }, nil
}

// trigger simulates the provider detecting an out-of-band change.
func (w *watchingProvider) trigger(c *Credential) {
	if w.cb != nil {
		w.cb(c)
	}
}

// fakeFactory is a ProviderFactory whose CreateProvider records the ConnInfo and
// config it received.
type fakeFactory struct {
	typ     string
	gotConn ConnInfo
	gotCfg  *fakeFactoryConfig
}

type fakeFactoryConfig struct {
	Region string `mapstructure:"region"`
}

func (f *fakeFactory) Type() string { return f.typ }

func (f *fakeFactory) CreateDefaultConfig() component.Config { return &fakeFactoryConfig{} }

func (f *fakeFactory) CreateProvider(_ ProviderSettings, cfg component.Config, conn ConnInfo) (Provider, error) {
	f.gotConn = conn
	f.gotCfg = cfg.(*fakeFactoryConfig)
	return &fakeProvider{cred: &Credential{Secret: "tok"}}, nil
}

func TestProvider_FakeSatisfiesInterface(t *testing.T) {
	var p Provider = &fakeProvider{cred: &Credential{Secret: "tok"}}
	got, err := p.GetCredential(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok", got.Secret)
}

func TestNopWatcher_EmbeddingSatisfiesWatch(t *testing.T) {
	// A struct embedding NopWatcher satisfies Provider without writing Watch.
	var p Provider = &fakeProvider{cred: &Credential{Secret: "x"}}
	called := false
	stop, err := p.Watch(context.Background(), func(*Credential) { called = true })
	require.NoError(t, err)
	require.NotNil(t, stop)
	assert.NotPanics(t, stop)
	assert.False(t, called, "NopWatcher must never invoke the callback")
}

func TestWatch_CallbackAndStop(t *testing.T) {
	w := &watchingProvider{}
	var got *Credential
	stop, err := w.Watch(context.Background(), func(c *Credential) { got = c })
	require.NoError(t, err)

	fresh := &Credential{Secret: "rotated"}
	w.trigger(fresh)
	assert.Same(t, fresh, got, "callback must receive the pushed credential")

	stop()
	assert.True(t, w.stopped, "stop must halt the watch")
}

func TestCredential_UsernameNilVsEmpty(t *testing.T) {
	empty := ""
	withEmpty := &Credential{Username: &empty}
	withNil := &Credential{Username: nil}

	require.NotNil(t, withEmpty.Username, "pointer to empty string is not nil")
	assert.Equal(t, "", *withEmpty.Username)
	assert.Nil(t, withNil.Username, "nil means: use the consumer's configured username")
}

func TestCredential_NotAfterNilVsSet(t *testing.T) {
	noExpiry := &Credential{Secret: "static"}
	assert.Nil(t, noExpiry.NotAfter, "nil NotAfter means no expiry applies")

	exp := time.Unix(1000, 0)
	withExpiry := &Credential{Secret: "token", NotAfter: &exp}
	require.NotNil(t, withExpiry.NotAfter)
	assert.Equal(t, exp, *withExpiry.NotAfter)
}

func TestNewProviderFactoryMap(t *testing.T) {
	f := &fakeFactory{typ: "aws_iam"}
	m, err := newProviderFactoryMap([]ProviderFactory{f})
	require.NoError(t, err)
	assert.Same(t, f, m["aws_iam"])
}

func TestNewProviderFactoryMap_Duplicate(t *testing.T) {
	_, err := newProviderFactoryMap([]ProviderFactory{
		&fakeFactory{typ: "aws_iam"},
		&fakeFactory{typ: "aws_iam"},
	})
	require.ErrorIs(t, err, errDuplicateFactory)
}

func TestNewProviderFactoryMap_InvalidType(t *testing.T) {
	// Empty and malformed type strings are both rejected by component.NewType.
	for _, typ := range []string{"", "aws-iam", "1aws"} {
		_, err := newProviderFactoryMap([]ProviderFactory{&fakeFactory{typ: typ}})
		require.ErrorIs(t, err, errInvalidFactoryType, "type %q should be rejected", typ)
	}
}
