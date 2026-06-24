package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	artifactType       = "application/vnd.opencapital.plugin.v1+json"
	configMediaType    = "application/vnd.opencapital.footprint.v1+json"
	layerMediaType     = "application/vnd.opencapital.plugin.tarball.v1.tar+gzip"
	platformAnnotation = "io.opencapital.platform"
)

// run assembles an OCI artifact from the plugin dir and pushes it to
// ghcr.io/<owner>/plugins/<id>:<version>. Returns the manifest digest.
func run(ctx context.Context, pluginDir, id, owner, version, token string, platforms []string) (string, error) {
	pjPath := filepath.Join(pluginDir, "dist", "plugin.json")
	pjBytes, err := os.ReadFile(pjPath)
	if err != nil {
		return "", fmt.Errorf("read %s (plugin must be built first): %w", pjPath, err)
	}
	fp, err := footprintFromPluginJSON(pjBytes)
	if err != nil {
		return "", fmt.Errorf("derive footprint: %w", err)
	}
	cfgBytes, err := json.Marshal(fp)
	if err != nil {
		return "", fmt.Errorf("marshal footprint: %w", err)
	}

	store := memory.New()

	configDesc := content.NewDescriptorFromBytes(configMediaType, cfgBytes)
	if err := store.Push(ctx, configDesc, bytes.NewReader(cfgBytes)); err != nil {
		return "", fmt.Errorf("push config blob: %w", err)
	}

	// Mirror repo-root dashboards/ + library-panels/ into dist/ once, before
	// packaging any platform, so every per-platform tarball carries them. The
	// footprint above is derived from plugin.json only, so this does not change
	// it (promotion's footprint gate is unaffected).
	if err := stageRootArtifacts(pluginDir); err != nil {
		return "", fmt.Errorf("stage root artifacts: %w", err)
	}

	var layers []ocispec.Descriptor
	for _, p := range platforms {
		fmt.Fprintf(os.Stderr, ">>> packaging %s %s %s\n", id, version, p)
		art, err := packagePlugin(pluginDir, id, version, p)
		if err != nil {
			return "", err
		}
		tarBytes, err := os.ReadFile(art.tarball)
		if err != nil {
			return "", fmt.Errorf("read tarball %s: %w", art.tarball, err)
		}
		desc := content.NewDescriptorFromBytes(layerMediaType, tarBytes)
		desc.Annotations = map[string]string{
			ocispec.AnnotationTitle: filepath.Base(art.tarball),
			platformAnnotation:      p,
		}
		if err := store.Push(ctx, desc, bytes.NewReader(tarBytes)); err != nil {
			return "", fmt.Errorf("push layer %s: %w", p, err)
		}
		layers = append(layers, desc)
	}

	manifestDesc, err := oras.PackManifest(ctx, store, oras.PackManifestVersion1_1, artifactType, oras.PackManifestOptions{
		ConfigDescriptor: &configDesc,
		Layers:           layers,
	})
	if err != nil {
		return "", fmt.Errorf("pack manifest: %w", err)
	}
	if err := store.Tag(ctx, manifestDesc, version); err != nil {
		return "", fmt.Errorf("tag manifest: %w", err)
	}

	repoRef := fmt.Sprintf("ghcr.io/%s/plugins/%s", owner, id)
	repo, err := remote.NewRepository(repoRef)
	if err != nil {
		return "", fmt.Errorf("repository %q: %w", repoRef, err)
	}

	host := "ghcr.io"
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: auth.StaticCredential(host, auth.Credential{
			Username: owner,
			Password: token,
		}),
	}

	fmt.Fprintf(os.Stderr, ">>> push -> %s:%s\n", repoRef, version)
	if _, err := oras.Copy(ctx, store, version, repo, version, oras.DefaultCopyOptions); err != nil {
		return "", fmt.Errorf("push %s:%s: %w", repoRef, version, err)
	}

	digest := manifestDesc.Digest.String()
	fmt.Fprintf(os.Stderr, ">>> pushed digest: %s\n", digest)
	return digest, nil
}
