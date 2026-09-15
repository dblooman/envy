// Package registry verifies immutable OCI artifacts with the server's Docker
// credential configuration (including configured credential helpers).
package registry

import (
	"context"
	"time"

	"github.com/dblooman/envy/internal/domain"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Provider struct{}

func (Provider) Check(ctx context.Context, image string) error {
	ref, err := name.NewDigest(image, name.StrictValidation)
	if err != nil {
		return domain.Validation("invalid digest-pinned image")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	desc, err := remote.Head(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return &domain.Error{Code: "unavailable", Message: "image digest cannot be read from registry; check retention and registry credentials", Retryable: true}
	}

	if desc.Digest.String() != ref.DigestStr() {
		return domain.Validation("registry returned a different image digest")
	}

	return nil
}
