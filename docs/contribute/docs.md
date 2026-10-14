# Editing the docs

The site is built with MkDocs and the Material theme from `docs/` and
`mkdocs.yml`.

```sh
python3 -m venv .venv
.venv/bin/pip install -r requirements-docs.txt
.venv/bin/mkdocs serve              # live preview at http://127.0.0.1:8000
.venv/bin/mkdocs build --strict     # the check CI runs
```

Rules that keep pages rendering the same on GitHub and on the site:

- plain GitHub-flavoured Markdown, `<details>` blocks and images only;
- every diagram is an Excalidraw drawing; no Mermaid or other diagram syntax;
- callouts use GitHub alert syntax (`> [!NOTE]`, `> [!WARNING]`);
- links to other pages are relative (`[Architecture](../concepts/architecture.md)`);
- one H1 per page.

## JSON examples

Every `json` block must parse and must pass `apranik check` once placed in a
config. Indent with two spaces, and keep an object on one line only when it holds
plain values and is short.

## Editing an Excalidraw diagram

Every diagram in `docs/assets/diagrams/` is an `.excalidraw.svg` file: an ordinary
SVG with the Excalidraw scene embedded. One diagram shows one case.

1. Open [excalidraw.com](https://excalidraw.com) and load the file with **Open**.
2. Edit the drawing in the same style: white background, a bold first line in each
   box, yellow for what is outside Apranik, violet for core, green for modules,
   blue for the contract.
3. Export with **Export image → SVG**, **Embed scene** on, over the same file.
4. Update the image's alt text, which a screen reader reads instead.
