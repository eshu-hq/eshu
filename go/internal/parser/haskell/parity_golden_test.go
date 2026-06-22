package haskell

const haskellParityGolden = `{
  "classes": [
    {
      "dead_code_root_kinds": [
        "haskell.exported_type"
      ],
      "end_line": 11,
      "lang": "haskell",
      "line_number": 11,
      "name": "Worker",
      "semantic_kind": "data"
    },
    {
      "end_line": 12,
      "lang": "haskell",
      "line_number": 12,
      "name": "Wrapper",
      "semantic_kind": "newtype"
    },
    {
      "end_line": 13,
      "lang": "haskell",
      "line_number": 13,
      "name": "Alias",
      "semantic_kind": "type"
    },
    {
      "end_line": 14,
      "lang": "haskell",
      "line_number": 14,
      "name": "Fam",
      "semantic_kind": "data"
    },
    {
      "dead_code_root_kinds": [
        "haskell.exported_type"
      ],
      "end_line": 16,
      "lang": "haskell",
      "line_number": 16,
      "name": "Runner",
      "semantic_kind": "typeclass"
    }
  ],
  "function_calls": [
    {
      "call_kind": "haskell.function_call",
      "class_context": "Runner Worker",
      "context": "runTask",
      "full_name": "run",
      "lang": "haskell",
      "line_number": 20,
      "name": "run"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "main",
      "full_name": "run",
      "lang": "haskell",
      "line_number": 23,
      "name": "run"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "run",
      "full_name": "helper",
      "lang": "haskell",
      "line_number": 26,
      "name": "helper"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "run",
      "full_name": "T.pack",
      "lang": "haskell",
      "line_number": 26,
      "name": "pack"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "run",
      "full_name": "show",
      "lang": "haskell",
      "line_number": 26,
      "name": "show"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "run",
      "full_name": "helper",
      "lang": "haskell",
      "line_number": 28,
      "name": "helper"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "run",
      "full_name": "T.unpack",
      "lang": "haskell",
      "line_number": 28,
      "name": "unpack"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "run",
      "full_name": "value",
      "lang": "haskell",
      "line_number": 28,
      "name": "value"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "caller",
      "full_name": "run",
      "lang": "haskell",
      "line_number": 33,
      "name": "run"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "caller",
      "full_name": "otherwise",
      "lang": "haskell",
      "line_number": 34,
      "name": "otherwise"
    },
    {
      "call_kind": "haskell.function_call",
      "context": "caller",
      "full_name": "run",
      "lang": "haskell",
      "line_number": 34,
      "name": "run"
    }
  ],
  "functions": [
    {
      "class_context": "Runner",
      "dead_code_root_kinds": [
        "haskell.typeclass_method"
      ],
      "decorators": [],
      "end_line": 17,
      "lang": "haskell",
      "line_number": 17,
      "name": "runTask"
    },
    {
      "class_context": "Runner Worker",
      "dead_code_root_kinds": [
        "haskell.instance_method"
      ],
      "decorators": [],
      "end_line": 20,
      "lang": "haskell",
      "line_number": 20,
      "name": "runTask",
      "source": "  runTask worker = run worker"
    },
    {
      "dead_code_root_kinds": [
        "haskell.main_function",
        "haskell.module_export"
      ],
      "decorators": [],
      "end_line": 23,
      "lang": "haskell",
      "line_number": 23,
      "name": "main",
      "source": "main = run Worker"
    },
    {
      "dead_code_root_kinds": [
        "haskell.module_export"
      ],
      "decorators": [],
      "end_line": 28,
      "lang": "haskell",
      "line_number": 26,
      "name": "run",
      "source": "run worker = helper (T.pack (show worker))"
    },
    {
      "decorators": [],
      "end_line": 30,
      "lang": "haskell",
      "line_number": 30,
      "name": "topVar",
      "source": "topVar = 42"
    },
    {
      "decorators": [],
      "end_line": 34,
      "is_dependency": false,
      "lang": "haskell",
      "line_number": 32,
      "name": "caller",
      "source": "caller value\n  | value > 0 = run value\n  | otherwise = run 0"
    }
  ],
  "imports": [
    {
      "alias": "T",
      "lang": "haskell",
      "line_number": 8,
      "name": "Data.Text"
    },
    {
      "lang": "haskell",
      "line_number": 9,
      "name": "Data.List"
    }
  ],
  "is_dependency": false,
  "lang": "haskell",
  "modules": [
    {
      "end_line": 6,
      "lang": "haskell",
      "line_number": 1,
      "name": "Demo.App"
    }
  ],
  "variables": []
}`
