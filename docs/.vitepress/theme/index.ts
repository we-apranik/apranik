import DefaultTheme from "vitepress/theme";
import { nextTick, onMounted, watch } from "vue";
import { useRoute } from "vitepress";
import type { Theme } from "vitepress";
import mediumZoom from "medium-zoom";
import { enhanceAppWithTabs } from "vitepress-plugin-tabs/client";
import "virtual:group-icons.css";
import "@fontsource-variable/jetbrains-mono";
import "@fontsource-variable/space-grotesk";
import "@fontsource-variable/instrument-sans";
import HomePage from "./HomePage.vue";
import "./style.css";

export default {
  extends: DefaultTheme,
  // A diagram is a link to its own file; zooming replaces following it.
  setup() {
    const route = useRoute();
    const zoomDiagrams = () => {
      document.querySelectorAll(".vp-doc a > img").forEach((image) => {
        image.parentElement?.addEventListener("click", (event) => event.preventDefault(), true);
      });
      mediumZoom(".vp-doc img", { background: "var(--vp-c-bg)" });
    };
    onMounted(zoomDiagrams);
    watch(() => route.path, () => nextTick(zoomDiagrams));
  },
  enhanceApp({ app }) {
    enhanceAppWithTabs(app);
    app.component("HomePage", HomePage);
  },
} satisfies Theme;
