// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configcredentials

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/component"
)

// erroringFactory is a ProviderFactory whose CreateProvider always fails.
type erroringFactory struct {
	typ string
	err error
}

func (f *erroringFactory) Type() string { return f.typ }

func (f *erroringFactory) CreateDefaultConfig() component.Config { return &fakeFactoryConfig{} }

func (f *erroringFactory) CreateProvider(ProviderSettings, component.Config, ConnInfo) (Provider, error) {
	return nil, f.err
}

func TestAuthentication_IsEmpty(t *testing.T) {
	assert.True(t, Authentication{}.IsEmpty())
	assert.False(t, Authentication{Settings: map[string]any{"aws_iam": map[string]any{}}}.IsEmpty())
}

func TestAuthentication_Validate(t *testing.T) {
	require.NoError(t, Authentication{}.Validate())
	require.NoError(t, Authentication{Settings: map[string]any{"aws_iam": nil}}.Validate())

	err := Authentication{Settings: map[string]any{"aws_iam": nil, "file": nil}}.Validate()
	require.ErrorIs(t, err, errMultipleAuthTypes)
}

func TestAuthentication_Resolve_SingleKey(t *testing.T) {
	f := &fakeFactory{typ: "aws_iam"}
	auth := Authentication{Settings: map[string]any{
		"aws_iam": map[string]any{"region": "ap-northeast-2"},
	}}

	conn := ConnInfo{Endpoint: "db:5432", Username: "monitor"}
	p, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{f}, conn)
	require.NoError(t, err)
	require.NotNil(t, p)

	// ConnInfo is passed through verbatim.
	assert.Equal(t, conn, f.gotConn)
	// Sub-config is unmarshaled into the factory's config.
	require.NotNil(t, f.gotCfg)
	assert.Equal(t, "ap-northeast-2", f.gotCfg.Region)
}

func TestAuthentication_Resolve_OptOutWhenEmpty(t *testing.T) {
	p, err := Authentication{}.Resolve(ProviderSettings{}, []ProviderFactory{&fakeFactory{typ: "aws_iam"}}, ConnInfo{})
	require.NoError(t, err)
	assert.Nil(t, p, "no auth type configured resolves to no provider, no error")
}

func TestAuthentication_Resolve_UnknownType(t *testing.T) {
	auth := Authentication{Settings: map[string]any{"vault": map[string]any{}}}
	_, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{&fakeFactory{typ: "aws_iam"}}, ConnInfo{})
	require.ErrorIs(t, err, errUnknownAuthType)
}

func TestAuthentication_Resolve_MultipleTypes(t *testing.T) {
	auth := Authentication{Settings: map[string]any{
		"aws_iam": map[string]any{},
		"vault":   map[string]any{},
	}}
	_, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{&fakeFactory{typ: "aws_iam"}}, ConnInfo{})
	require.ErrorIs(t, err, errMultipleAuthTypes)
}

func TestAuthentication_Resolve_DuplicateFactories(t *testing.T) {
	auth := Authentication{Settings: map[string]any{"aws_iam": map[string]any{}}}
	_, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{
		&fakeFactory{typ: "aws_iam"},
		&fakeFactory{typ: "aws_iam"},
	}, ConnInfo{})
	require.ErrorIs(t, err, errDuplicateFactory)
}

func TestAuthentication_Resolve_NoBody(t *testing.T) {
	// "aws_iam:" with no sub-config still resolves; the factory gets its default config.
	f := &fakeFactory{typ: "aws_iam"}
	auth := Authentication{Settings: map[string]any{"aws_iam": nil}}
	p, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{f}, ConnInfo{})
	require.NoError(t, err)
	require.NotNil(t, p)
	require.NotNil(t, f.gotCfg)
	assert.Equal(t, "", f.gotCfg.Region)
}

func TestAuthentication_Resolve_CreateProviderError(t *testing.T) {
	sentinel := errors.New("mint failed")
	auth := Authentication{Settings: map[string]any{"aws_iam": map[string]any{}}}
	_, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{
		&erroringFactory{typ: "aws_iam", err: sentinel},
	}, ConnInfo{})
	require.ErrorIs(t, err, sentinel)
}

func TestAuthentication_Resolve_UnmarshalError(t *testing.T) {
	// region is a string field; an int value fails to unmarshal.
	auth := Authentication{Settings: map[string]any{
		"aws_iam": map[string]any{"region": map[string]any{"nested": "notastring"}},
	}}
	_, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{&fakeFactory{typ: "aws_iam"}}, ConnInfo{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal config")
}

func TestAuthentication_Resolve_InvalidFactoryType(t *testing.T) {
	// A factory whose Type() is not a valid component type must error, not panic.
	auth := Authentication{Settings: map[string]any{"aws_iam": map[string]any{}}}
	_, err := auth.Resolve(ProviderSettings{}, []ProviderFactory{&fakeFactory{typ: "aws-iam"}}, ConnInfo{})
	require.ErrorIs(t, err, errInvalidFactoryType)
}
