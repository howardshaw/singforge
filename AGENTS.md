# SingForge

- For deployments, use a tagged GitHub Release and verify its checksums. Follow the installation section in README.md or README.zh-CN.md. Keep the operator's env and credential files private (0600).
- Inspect the target's existing installation, listeners and SSH port. Preview with `singforge provision --env-file PATH --dry-run`, then apply the same file. Use the requested zone and a prefix unique to the host.
- Verify `singforge doctor` and the enabled Tunnel and certificate renewal status. For requested subscription URLs, use `singforge subscription issue --format FORMAT` with `mihomo`, `shadowsocks` or `v2ray`. Preserve existing grants. Test each URL over HTTPS and deliver bearer links privately to the operator.
- For code changes, run `make check`. Use the integration and systemd workflows for changes to generated configurations or host lifecycle. Publish releases through GitHub Actions.
