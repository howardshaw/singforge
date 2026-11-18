# Architecture

The executable enters through `cmd/singforge/main.go`. The Cobra tree in `internal/cli/root.go` attaches each leaf to an operation. Host operations take an exclusive file lock. The private JSON state carries a schema version, protocol credentials, endpoint references and routing policy.

```mermaid
flowchart LR
    Operator --> Cobra
    Cobra --> State[(Private state)]
    State --> Render[Configuration rendering]
    Render --> Validator[Core and origin validators]
    Validator --> Systemd
    Systemd --> SB[sing-box]
    Systemd --> CF[cloudflared]
    Systemd --> NG[Nginx]
    Systemd --> Sub[Optional subscription publisher]
    Client --> SB
```

## Access modes

```mermaid
flowchart LR
    Client -->|Direct TCP or UDP| Public[Public sing-box listeners]
    Client -->|HTTPS WS| Edge[Cloudflare edge]
    Edge -->|CDN HTTPS| NG[Dedicated Nginx]
    Edge -->|Named Tunnel| CF[cloudflared]
    NG -->|exact paths| WS[Loopback WS inbounds]
    CF -->|anchored path patterns| WS
```

## Installation

```mermaid
sequenceDiagram
    actor Operator
    Operator->>CLI: install
    CLI->>Host: preflight and flock
    CLI->>GitHub: fetch digest-pinned binary
    CLI->>Generation: write private candidate files
    CLI->>Validator: sing-box check and optional nginx -t
    CLI->>Host: journal previous generation and swap current symlink
    CLI->>systemd: reload units and start services
    CLI->>Host: verify PID-owned sockets and tunnel readiness
    CLI->>Host: commit or restore journaled generation
```

## Subscriptions

```mermaid
flowchart LR
    Inbounds --> Nodes
    Endpoints --> Nodes
    Nodes --> Format[Capability filter and renderer]
    Format --> Local[Private file or terminal QR]
    Format --> Bundle[Private publisher bundle]
    Grant[Token digest and expiry] --> Bundle
    Browser -->|HTTPS| NG[Nginx]
    NG -->|loopback| Auth[Validate host, path and token]
    Bundle --> Auth
    Auth --> ClientConfig
```

The publisher receives rendered exports and grant hashes over a systemd-opened input file. It needs neither the deployment lock nor read access to server private keys.

## Update and rollback

```mermaid
flowchart TD
    Fetch[Fetch and verify] --> Stage[Stage executable]
    Stage --> Check[Validate candidate configuration]
    Check --> Swap[Switch active generation]
    Swap --> Ready{Service ready?}
    Ready -->|yes| Commit
    Ready -->|no| Restore[Restore prior generation and services]
    Interrupted[Interrupted operation] --> Journal[Read transaction journal]
    Journal --> Restore
```

## Endpoint lifecycle

```mermaid
stateDiagram-v2
    [*] --> Defined: create inbound credentials
    Defined --> Enabled: attach endpoint
    Enabled --> Enabled: change host or port
    Enabled --> Disabled: disable inbound
    Disabled --> Enabled: reuse credentials
    Enabled --> Detached: remove endpoint
    Detached --> Enabled: attach endpoint
```

`internal/model` owns these relations. `internal/render` produces native client and server shapes. `internal/cloudflare` handles API reconciliation. `internal/host` owns generated services, binaries, readiness and rollback. `internal/store` implements locking, atomic file replacement and portable state backup.
