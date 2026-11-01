package container

import (
	"net/http"

	"oras.land/oras-go/v2/registry/remote/auth"

	"github.com/k8s-manifest-kit/pkg/util/option"
)

// ClientOption is a functional option for the OCI Client.
type ClientOption = option.Option[ClientOptions]

// ClientOptions holds configuration for the OCI registry client.
type ClientOptions struct {
	credential *auth.Credential
	httpClient *http.Client
	PlainHTTP  bool
}

// ApplyTo applies the client options to the target configuration.
func (opts ClientOptions) ApplyTo(target *ClientOptions) {
	if opts.credential != nil {
		target.credential = opts.credential
	}

	if opts.httpClient != nil {
		target.httpClient = opts.httpClient
	}

	if opts.PlainHTTP {
		target.PlainHTTP = opts.PlainHTTP
	}
}

// WithCredential sets the authentication credentials for the OCI client.
func WithCredential(username string, password string) ClientOption {
	return option.FunctionalOption[ClientOptions](func(opts *ClientOptions) {
		opts.credential = &auth.Credential{
			Username: username,
			Password: password,
		}
	})
}

// WithPlainHTTP enables plain HTTP (non-TLS) communication with the registry.
func WithPlainHTTP(plain bool) ClientOption {
	return option.FunctionalOption[ClientOptions](func(opts *ClientOptions) {
		opts.PlainHTTP = plain
	})
}

// WithHTTPClient sets the underlying HTTP client used for registry
// communication. When nil, the ORAS default is used, which shares
// http.DefaultTransport. Supplying a client with a private transport is
// required when concurrent httptest servers are closed while requests are
// still in flight, because httptest.Server.Close closes idle connections on
// http.DefaultTransport process-wide.
func WithHTTPClient(client *http.Client) ClientOption {
	return option.FunctionalOption[ClientOptions](func(opts *ClientOptions) {
		opts.httpClient = client
	})
}
