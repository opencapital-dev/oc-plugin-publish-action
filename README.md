# oc-plugin-publish-action

Composite GitHub Action that assembles a Grafana plugin into an OpenCapital OCI
artifact, pushes and signs it to `ghcr.io/<owner>/plugins/<id>:<version>`, and
records the new version in the plugin's own `oc-plugin.json` manifest on `main`.

The artifact structure:

- **Config blob** (`application/vnd.opencapital.footprint.v1+json`): the plugin
  Footprint derived from `dist/plugin.json` — what control-plane reads at install
  time.
- **Layers** (`application/vnd.opencapital.plugin.tarball.v1.tar+gzip`): one
  per platform, annotated `io.opencapital.platform=<os-arch>`.
- **Artifact type**: `application/vnd.opencapital.plugin.v1+json`

After the push, the action signs the manifest digest using cosign with the
caller's GitHub Actions OIDC identity against public Sigstore (keyless,
`--new-bundle-format`).

Finally, the action commits an updated `oc-plugin.json` to the caller repo's
`main` branch via the built-in `GITHUB_TOKEN` (own-repo write — no PAT, no
cross-repo, no PR). The `versions[]` array is kept v-prefixed and sorted
semver-descending. The manifest is created automatically on first publish.

## Inputs

| Input       | Required | Default                                                           | Description                                         |
|-------------|----------|-------------------------------------------------------------------|-----------------------------------------------------|
| `dir`       | yes      | `.`                                                               | Plugin source directory (must contain `dist/`)      |
| `id`        | yes      |                                                                   | Plugin id (OCI repo name, e.g. `oc-core-datasource`) |
| `owner`     | yes      |                                                                   | GitHub owner/org                                    |
| `version`   | yes      |                                                                   | OCI tag, e.g. `v0.1.0`                              |
| `platforms` | no       | `linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64` | Comma-separated platform list                       |
| `record_manifest` | no | `true` | When `false`, skip the `oc-plugin.json` `versions[]` bump (caller records it) |

## Outputs

| Output   | Description                     |
|----------|---------------------------------|
| `digest` | Pushed manifest digest (`sha256:...`) |

## Permissions required

The caller job must grant:

```yaml
permissions:
  packages: write    # push OCI artifact + cosign signature to GHCR
  id-token: write    # cosign keyless OIDC signing via Sigstore
  contents: write    # bump manifest versions[] in oc-plugin.json on main
```

## Usage

```yaml
jobs:
  publish:
    permissions:
      packages: write
      id-token: write
      contents: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - name: Build plugin
        run: npm ci && npm run build && mage buildAll
      - uses: opencapital-dev/oc-plugin-publish-action@v1
        with:
          dir: .
          id: oc-core-datasource
          owner: ${{ github.repository_owner }}
          version: v${{ env.VERSION }}
```

After a successful run the image is at
`ghcr.io/<owner>/plugins/<id>:<version>` and the caller repo's
`oc-plugin.json` on `main` contains the new version in `versions[]`.
