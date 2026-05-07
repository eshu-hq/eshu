import express from "express";

const router = express.Router();

export function publicApi() {
  return "public";
}

function main() {
  return formatUser({ name: "Asha" });
}

function formatUser(user) {
  return user.name.toUpperCase();
}

function unusedLocalHelper() {
  return "unused";
}

function loginHandler(req, res) {
  return res.json({ ok: true });
}

function authMiddleware(req, res, next) {
  return next();
}

function listUsers(req, res) {
  return res.json([]);
}

function dynamicDispatchTarget() {
  return "dynamic";
}

function ambiguousPropertyTarget() {
  return "ambiguous";
}

const handlers = {
  dynamicDispatchTarget,
  ambiguousPropertyTarget,
};

async function semanticDispatch(name) {
  const moduleName = `./plugins/${name}.js`;
  const plugin = await import(moduleName);
  return handlers[name]?.() ?? plugin.default?.();
}

router.post("/login", loginHandler);
router.get("/users", [authMiddleware], listUsers);

module.exports = {
  publicApi,
  main,
  semanticDispatch,
};
