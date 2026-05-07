import importlib


def semantic_dispatch_target() -> str:
    return "semantic"


def ambiguous_dynamic_target() -> str:
    return "ambiguous"


def load_named_handler(module_name: str, handler_name: str):
    module = importlib.import_module(module_name)
    return getattr(module, handler_name)


def dispatch_known_handler():
    return load_named_handler("python.dynamic_loader", "semantic_dispatch_target")
