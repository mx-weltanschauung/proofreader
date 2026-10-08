# Proofreader — a reading-room platform

Proofreader turns scanned books (PDF, DJVU) into page-by-page Markdown that
people proofread and read online. Printed pagination is preserved: each page
on screen is the same page of the printed volume, with its scan beside it.

Features: shelves of editions and volumes; chapters with footnotes; page
versions and statuses; reader-submitted corrections with moderation;
full-text search (PostgreSQL FTS, Russian stemming); subject index; reader
collections and essays quoting the corpus by page; EPUB/FB2/Markdown/HTML
downloads; an OPDS catalog; crawler pages and link previews; plain-text
`.md` pages and `llms.txt` for language models; an MCP server; and a static
offline copy of the whole reading room.

Stack: Go backend (JSON API, PostgreSQL), React SPA, any S3-compatible
storage (SeaweedFS locally).

The interface, code comments and documentation are in **Russian** — the
platform was built for Russian-language collected works. There is no i18n
layer yet.

## Quick start

```bash
cp env.example .env
docker compose up --build
```

Open http://localhost:3100 and sign in as `admin@proofreader.local` /
`admin`. Your own instance on your own domain — name, description, contacts,
icons and legal page are configuration, not code — is described in
[docs/SELF_HOSTING.md](docs/SELF_HOSTING.md).

## License

[GNU AGPL-3.0](LICENSE). If you run a modified version as a network
service, you must offer its source to the users of that service.

Security issues: see [SECURITY.md](SECURITY.md).
