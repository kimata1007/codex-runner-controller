# Repository agent instructions

## Version selection

- Prefer the latest stable version of languages, libraries, tools, protocols, and container images when making a new choice or upgrading an existing dependency.
- Verify that the selected version is available and compatible with this repository at the time of the change.
- Pin versions explicitly. Do not use floating tags or selectors such as `latest`, unbounded version ranges, or image tags that can change over time.
- When upgrading, update the version declaration and the relevant lock/checksum files together, and mention the pinned version in the change summary.
