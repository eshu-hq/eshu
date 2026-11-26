import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";

import { App } from "./App";
import { applyStoredTheme } from "./theme/theme";

applyStoredTheme();

const root = document.getElementById("root");

if (root === null) {
  throw new Error("Eshu Console root element is missing");
}

createRoot(root).render(
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
);
