# Helm Chart Release

## Chart Location
- `charts/openchat-backend`

## OCI Publish Workflow
- Workflow file: `.github/workflows/publish-helm-chart.yml`
- Trigger: git tag push matching `chart-vX.X.X`

## Release Behavior
1. Validate tag format strictly as `chart-vX.X.X`.
2. Convert the chart version to semver `X.X.X` for Helm packaging.
3. Publish chart to GHCR OCI:
   - `ghcr.io/<owner>/charts/openchat-backend:X.X.X`
4. Add OCI alias tag:
   - `ghcr.io/<owner>/charts/openchat-backend:chart-vX.X.X`

## Why There Are Two Tags
Helm chart metadata requires semver. `chart-vX.X.X` is not a valid chart version string, so the workflow publishes semver for Helm compatibility and then adds the `chart-vX.X.X` OCI alias for release traceability.

## Example
Release tag:
- `chart-v0.2.0`

Published refs:
- `ghcr.io/<owner>/charts/openchat-backend:0.2.0`
- `ghcr.io/<owner>/charts/openchat-backend:chart-v0.2.0`
