import DefaultTheme from "vitepress/theme";
import type { Theme } from "vitepress";
import { enhanceAppWithTabs } from "vitepress-plugin-tabs/client";
import "@fontsource-variable/jetbrains-mono";
import "@fontsource-variable/space-grotesk";
import "@fontsource-variable/instrument-sans";
import HomePage from "./HomePage.vue";
import "./style.css";

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    enhanceAppWithTabs(app);
    app.component("HomePage", HomePage);
  },
} satisfies Theme;
