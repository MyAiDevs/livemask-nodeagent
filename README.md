# LiveMask NodeAgent

> VPN Node Agent with sing-box, Degraded Mode, One-click Install, Quality Reporting

## Quick Start

```bash
git clone https://github.com/MyAiDevs/livemask-nodeagent.git
cd livemask-nodeagent

git submodule add https://github.com/MyAiDevs/livemask-docs.git docs
git submodule update --init --recursive
bash scripts/sync-ai-rules.sh
```

## AI Development Rules

This repo uses centralized rules from livemask-docs.

Key modules for NodeAgent:
- NodeAgent Specific Rules
- Security & Secrets
- Config Hot Update
- Production Automation

## Cross-Repo Linkage

- Config & hot update from Backend
- Quality reporting to Backend & Recommendation Engine

## Protocol Profile Support Matrix

| Profile | Status | Notes |
|---------|--------|-------|
| mixed | implemented | SOCKS5 + HTTP proxy inbound |
| socks | implemented | SOCKS5 proxy inbound |
| tun | implemented | TUN device + mixed service inbound |
| hysteria2 | implemented | Hysteria2 server inbound (TASK-NODEAGENT-HYSTERIA2-001) |
| vless_reality | reserved | Not yet implemented |
| trojan | reserved | Not yet implemented |
| shadowtls | reserved | Not yet implemented |
| wireguard | reserved | Not yet implemented |

### Hysteria2 Profile Details

Hysteria2 is the first "real" protocol profile implemented via the ProtocolProfile plugin architecture (TASK-NODEAGENT-HYSTERIA2-001).

**Required secrets:**
- `HYSTERIA2_AUTH` env: authentication password for the hysteria2 server
- `HYSTERIA2_OBFS_PASSWORD` env: obfuscation password (only required when obfs is enabled)

**Supported fields (via SingboxConfig Raw / env):**
- `hysteria2_up_mbps`: upstream bandwidth limit (Mbps)
- `hysteria2_down_mbps`: downstream bandwidth limit (Mbps)
- `hysteria2_obfs_type`: obfuscation type (`obfs` or `obfs_tls`)
- `hysteria2_auth_env`: custom env var name for auth secret
- `hysteria2_obfs_password_env`: custom env var name for obfs password

**Notes:**
- Secret values must be provided via environment variables (not hardcoded in config).
- Errors, logs, and status never contain secret values (redacted via RedactProtocolConfig).
- Config files are written with 0600 permissions via atomic temp+rename.
- This implementation does NOT include App native tunnel support — that is a separate task.
- This implementation does NOT change the Backend API contract.

**Future protocols** must follow the same ProtocolProfile interface. Do not add protocol-specific if/else logic in the renderer.

Central docs: https://github.com/MyAiDevs/livemask-docs
