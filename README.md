# agent-code-review

`agent-code-review` is a CLI written in Go and shipped as an agent plugin. It reviews a pull or merge request and, in cascade, every change listed in its `## Related Changes` section, each one with the review criteria of its own repository. It writes a full report per change to a temporary work dir, shows only a summary in chat, and writes to the platform only on explicit request.

Status: under development, not usable yet.
