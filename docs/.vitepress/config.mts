import { cpSync } from "node:fs";
import { join } from "node:path";
import { defineConfig } from "vitepress";
import llmstxt from "vitepress-plugin-llms";
import { groupIconMdPlugin, groupIconVitePlugin } from "vitepress-plugin-group-icons";
import { tabsMarkdownPlugin } from "vitepress-plugin-tabs";
import { apranikTheme } from "./apranikJsonTheme.mts";
import { configFieldCards } from "./configFieldCards.mts";
import { addPageMetadata, siteAddress, siteWideHead } from "./pageMetadata.mts";
import { navigation, sidebar } from "./sidebar.mts";

const repositoryUrl = "https://github.com/we-apranik/apranik";

export default defineConfig({
  title: "Apranik",
  description: "An embeddable, open-source proxy core for censorship circumvention.",
  lang: "en-US",
  base: "/apranik/",
  cleanUrls: true,
  appearance: "force-dark",
  srcExclude: ["_plan.md", "_report.md", "context/**", "plans/**"],
  head: siteWideHead,
  sitemap: { hostname: siteAddress },
  transformPageData(pageData, { siteConfig }) {
    addPageMetadata(pageData, siteConfig);
  },
  // A diagram is also linked to as a file, which the bundler would not keep
  // under its own name; copy them so those links open the drawing itself.
  buildEnd(siteConfig) {
    cpSync(
      join(siteConfig.srcDir, "assets", "diagrams"),
      join(siteConfig.outDir, "assets", "diagrams"),
      { recursive: true },
    );
  },
  markdown: {
    theme: apranikTheme,
    config(md) {
      md.use(tabsMarkdownPlugin);
      md.use(groupIconMdPlugin);
      md.use(configFieldCards);
    },
  },
  vite: {
    plugins: [
      groupIconVitePlugin(),
      llmstxt({
        ignoreFiles: ["_plan.md", "_report.md", "context/**", "plans/**"],
        title: "Apranik",
        description:
          "An embeddable, open-source proxy core for censorship circumvention, written in Go.",
      }),
    ],
  },
  themeConfig: {
    logo: "/logo.svg",
    nav: navigation,
    sidebar,
    socialLinks: [{ icon: "github", link: repositoryUrl }],
    editLink: {
      pattern: `${repositoryUrl}/edit/main/docs/:path`,
      text: "Edit this page on GitHub",
    },
    search: { provider: "local" },
    outline: { level: [2, 3] },
  },
});
