import { StrictMode } from "react";
import "@/i18n";
import { createRoot } from "react-dom/client";
import { RouterProvider } from "react-router";
import { AppProviders } from "./app/providers";
import { router } from "./app/router";
import "@/styles/index.css";

const container = document.getElementById("root");
if (!container) {
  throw new Error("缺少 #root 挂载点");
}

createRoot(container).render(
  <StrictMode>
    <AppProviders>
      <RouterProvider router={router} />
    </AppProviders>
  </StrictMode>,
);
