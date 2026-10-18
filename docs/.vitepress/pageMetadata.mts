import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { HeadConfig, PageData, SiteConfig } from "vitepress";

export const siteAddress = "https://we-apranik.github.io/apranik/";
const siteName = "Apranik";
const maximumDescriptionLength = 160;

export const siteWideHead: HeadConfig[] = [
  ["link", { rel: "icon", href: "/apranik/logo.svg", type: "image/svg+xml" }],
  ["link", { rel: "apple-touch-icon", href: "/apranik/apple-touch-icon.png" }],
  ["link", { rel: "manifest", href: "/apranik/manifest.webmanifest" }],
  ["meta", { name: "theme-color", content: "#0e1013" }],
  ["meta", { name: "color-scheme", content: "dark" }],
];

function pageAddress(relativePath: string): string {
  const withoutExtension = relativePath.replace(/\.md$/, "");
  if (withoutExtension === "index") return siteAddress;
  return siteAddress + withoutExtension.replace(/\/index$/, "/");
}

function removeMarkdown(text: string): string {
  return text
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[`*_]/g, "")
    .replace(/<[^>]+>/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

function cutAtWord(text: string): string {
  if (text.length <= maximumDescriptionLength) return text;
  const cut = text.slice(0, maximumDescriptionLength - 1);
  return cut.slice(0, cut.lastIndexOf(" ")).replace(/[,;:.\s]+$/, "") + "…";
}

// The first paragraph of plain text after the title: not a heading, image,
// table, list, quote, container, code block or HTML.
export function firstParagraphOf(markdownSource: string): string | undefined {
  const withoutFrontmatter = markdownSource.replace(/^---\n[\s\S]*?\n---\n/, "");
  const withoutCodeBlocks = withoutFrontmatter.replace(/^```[\s\S]*?^```/gm, "");
  for (const block of withoutCodeBlocks.split(/\n\s*\n/)) {
    const trimmed = block.trim();
    if (trimmed === "" || /^(#|!\[|\[!\[|\||[-*>:<]|\d+\.)/.test(trimmed)) continue;
    const text = removeMarkdown(trimmed);
    if (text.length >= 40) return cutAtWord(text);
  }
  return undefined;
}

export function addPageMetadata(pageData: PageData, siteConfig: SiteConfig): void {
  const isHome = pageData.relativePath === "index.md";
  if (!pageData.frontmatter.description) {
    const source = readFileSync(join(siteConfig.srcDir, pageData.relativePath), "utf8");
    const derived = firstParagraphOf(source);
    if (derived) pageData.description = derived;
  } else {
    pageData.description = String(pageData.frontmatter.description);
  }
  const title = isHome ? `${siteName}: a proxy core` : `${pageData.title} | ${siteName}`;
  const address = pageAddress(pageData.relativePath);
  const imageAddress = siteAddress + "og-image.png";
  const head: HeadConfig[] = [
    ["meta", { property: "og:site_name", content: siteName }],
    ["meta", { property: "og:type", content: isHome ? "website" : "article" }],
    ["meta", { property: "og:title", content: title }],
    ["meta", { property: "og:description", content: pageData.description }],
    ["meta", { property: "og:url", content: address }],
    ["meta", { property: "og:image", content: imageAddress }],
    ["meta", { property: "og:image:width", content: "1200" }],
    ["meta", { property: "og:image:height", content: "630" }],
    ["meta", { property: "og:image:alt", content: "Apranik, an open-source proxy core for censorship circumvention" }],
    ["meta", { name: "twitter:card", content: "summary_large_image" }],
    ["meta", { name: "twitter:title", content: title }],
    ["meta", { name: "twitter:description", content: pageData.description }],
    ["meta", { name: "twitter:image", content: imageAddress }],
  ];
  pageData.frontmatter.head = [...(pageData.frontmatter.head ?? []), ...head];
}
