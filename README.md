# oc-plugin-publish-action

Composite GitHub Action that assembles a Grafana plugin into an OpenCapital OCI
artifact and pushes it to `ghcr.io/<owner>/plugins-staging/<id>:<version>`.

The artifact structure:

- **Config blob** (`application/vnd.opencapital.footprint.v1+json`): the plugin
  Footprint derived from `dist/plugin.json` — what control-plane reads at install
  time.
- **Layers** (`application/vnd.opencapital.plugin.tarball.v1.tar+gzip`): one
  per platform, annotated `io.opencapital.platform=<os-arch>`.
- **Artifact type**: `application/vnd.opencapital.plugin.v1+json`

After the push, the action signs the manifest digest using cosign with the
caller's GitHub Actions OIDC identity against public Sigstore (keyless,
`--new-bundle-format`). The caller's job must grant `id-token: write` and
`packages: write`.

## Inputs

| Input       | Required | Default                                                           | Description                                         |
|-------------|----------|-------------------------------------------------------------------|-----------------------------------------------------|
| `dir`       | yes      | `.`                                                               | Plugin source directory (must contain `dist/`)      |
| `id`        | yes      |                                                                   | Plugin id (OCI repo name, e.g. `oc-core-datasource`) |
| `owner`     | yes      |                                                                   | GitHub owner/org                                    |
| `version`   | yes      |                                                                   | OCI tag, e.g. `v0.1.0`                              |
| `platforms` | no       | `linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64` | Comma-separated platform list                       |

## Outputs

| Output   | Description                     |
|----------|---------------------------------|
| `digest` | Pushed manifest digest (`sha256:...`) |

## Usage

```yaml
jobs:
  publish:
    permissions:
      packages: write
      id-token: write
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
