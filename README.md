# LiveMask NodeAgent

> VPN Node Agent with sing-box, Degraded Mode, One-click Install, Quality Reporting

## Quick Start

```bash
git clone https://github.com/sammytan/livemask-nodeagent.git
cd livemask-nodeagent

git submodule add https://github.com/sammytan/livemask-docs.git docs
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

Central docs: https://github.com/sammytan/livemask-docs