import DefaultTheme from "vitepress/theme";
import type { Theme } from "vitepress";
import ModeGallery from "./ModeGallery.vue";
import ProjectHome from "./ProjectHome.vue";
import ArchitectureDiagram from "./ArchitectureDiagram.vue";
import "./style.css";

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component("ProjectHome", ProjectHome);
    app.component("ModeGallery", ModeGallery);
    app.component("ArchitectureDiagram", ArchitectureDiagram);
  },
} satisfies Theme;
