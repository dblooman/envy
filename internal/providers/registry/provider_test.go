package registry

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	ociregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestChecksExactArtifactAndRejectsMissingDigest(t *testing.T) {
	server := httptest.NewServer(ociregistry.New())
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	tag, err := name.NewTag(host + "/service:test")
	if err != nil {
		t.Fatal(err)
	}

	if err = remote.Write(tag, empty.Image); err != nil {
		t.Fatal(err)
	}

	digest, err := empty.Image.Digest()
	if err != nil {
		t.Fatal(err)
	}

	p := Provider{}
	if err = p.Check(context.Background(), host+"/service@"+digest.String()); err != nil {
		t.Fatal(err)
	}

	if err = p.Check(context.Background(), host+"/service@sha256:"+strings.Repeat("0", 64)); err == nil {
		t.Fatal("missing artifact accepted")
	}

	if err = p.Check(context.Background(), host+"/service:test"); err == nil {
		t.Fatal("mutable tag accepted")
	}
}
