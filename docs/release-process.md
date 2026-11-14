# Release process

Pull requests and pushes to `main` build all four project container images without publishing them. The runner-agent image uses the Codex CLI version in `CODEX_CLI_VERSION` so builds do not silently move to a newer CLI release.

On each push to `main`, the `tagpr` workflow creates or updates a release pull request. The release pull request proposes a SemVer version and updates the image tags in the Kustomize manifests. Add the `tagpr:minor` or `tagpr:major` label to the release pull request when the default patch increment is not appropriate.

When the release pull request is merged, `tagpr` creates the corresponding `vX.Y.Z` Git tag and GitHub Release. The same workflow reads the tag output and publishes all four images to GHCR with both the SemVer tag and a `sha-<commit>` tag. Keeping publication in the `tagpr` workflow is intentional: a tag created with `GITHUB_TOKEN` does not start a separate tag-triggered workflow.

The first run starts from `v0.0.0` if the repository has no SemVer tags, so its default proposal is `v0.0.1`. Add `tagpr:minor` to the initial release pull request to start at `v0.1.0` instead.

For `tagpr` to create its release pull request, enable **Allow GitHub Actions to create and approve pull requests** under the repository's Actions settings. The workflow uses the built-in `GITHUB_TOKEN`; it does not need a personal access token.

New GHCR packages are private by default. If the deployment cluster should pull the images without registry credentials, set each package's visibility to Public after its first publication. Otherwise, provide GHCR pull credentials to the cluster.
