package resolutionparity

import "github.com/eshu-hq/eshu/go/internal/codeprovenance"

func dartImportBindingCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "dart_import_binding",
		files: map[string]string{
			"lib/service.dart": `
import 'src/helper.dart';

class Runner {
  final value = Helper();
}
`,
			"lib/src/helper.dart": `
class Helper {
}
`,
			"lib/src/other_helper.dart": `
class Helper {
}
`,
		},
		caller: "Runner",
		callee: "Helper",
		method: codeprovenance.MethodImportBinding,
		uidByPath: map[string]string{
			"lib/service.dart:Runner":          "content-entity:dart_import_binding:Runner",
			"lib/src/helper.dart:Helper":       "content-entity:dart_import_binding:Helper",
			"lib/src/other_helper.dart:Helper": "content-entity:dart_import_binding:Helper_decoy",
		},
	}
}

func importBindingCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "python_import_binding",
		files: map[string]string{
			"app.py": `
from lib_a import helper as renamed

def caller():
    return renamed()
`,
			"lib_a.py": `
def helper():
    return "a"
`,
			"lib_b.py": `
def renamed():
    return "b"
`,
		},
		caller: "caller",
		callee: "helper",
		method: codeprovenance.MethodImportBinding,
		uidByPath: map[string]string{
			"lib_a.py:helper":  "content-entity:python_import_binding:helper",
			"lib_b.py:renamed": "content-entity:python_import_binding:helper_decoy",
		},
	}
}

func javaImportBindingCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "java_import_binding",
		files: map[string]string{
			"example/Worker.java": `
package example;

import com.acme.Service;

class Worker {
  void caller(Service service) {
    service.process(new Task());
  }
}
`,
			"com/acme/Service.java": `
package com.acme;

class Service {
  void process(Task task) {}
}

class Task {}
`,
			"com/other/Service.java": `
package com.other;

class Service {
  void process(Task task) {}
}

class Task {}
`,
		},
		caller: "caller",
		callee: "process",
		method: codeprovenance.MethodImportBinding,
		uidByPath: map[string]string{
			"com/acme/Service.java:process":  "content-entity:java_import_binding:process",
			"com/other/Service.java:process": "content-entity:java_import_binding:process_decoy",
		},
	}
}

func elixirImportBindingCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "elixir_import_binding",
		files: map[string]string{
			"lib/worker.ex": `
defmodule Demo.Worker do
  alias Demo.Context

  def caller do
    Context.Basic.greet()
  end
end
`,
			"lib/context/basic.ex": `
defmodule Demo.Context.Basic do
  def greet do
    :ok
  end
end
`,
			"lib/context_basic_decoy.ex": `
defmodule Context.Basic do
  def greet do
    :decoy
  end
end
`,
		},
		caller: "caller",
		callee: "greet",
		method: codeprovenance.MethodImportBinding,
		uidByPath: map[string]string{
			"lib/context/basic.ex:greet":       "content-entity:elixir_import_binding:greet",
			"lib/context_basic_decoy.ex:greet": "content-entity:elixir_import_binding:greet_decoy",
		},
	}
}

func groovyClassQualifiedCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "groovy_class_qualified",
		files: map[string]string{
			"vars/deployPipeline.groovy": `
def caller() {
  DeployHelper.deployApp('prod')
}
`,
			"src/org/example/DeployHelper.groovy": `
class DeployHelper {
  static def deployApp(String target) {
  }
}
`,
			"src/org/example/OtherHelper.groovy": `
class OtherHelper {
  static def deployApp(String target) {
  }
}
`,
		},
		caller: "caller",
		callee: "deployApp",
		method: codeprovenance.MethodTypeInferred,
		uidByPath: map[string]string{
			"src/org/example/DeployHelper.groovy:deployApp": "content-entity:groovy_class_qualified:deployApp",
			"src/org/example/OtherHelper.groovy:deployApp":  "content-entity:groovy_class_qualified:deployApp_decoy",
		},
	}
}

func haskellImportBindingCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "haskell_import_binding",
		files: map[string]string{
			"app/Main.hs": `
module Main where

import qualified Data.Text as T

caller value = T.pack value
`,
			"src/Data/Text.hs": `
module Data.Text where

pack value = value
`,
			"src/Other/Text.hs": `
module Other.Text where

pack value = value
`,
		},
		caller: "caller",
		callee: "pack",
		method: codeprovenance.MethodImportBinding,
		uidByPath: map[string]string{
			"src/Data/Text.hs:pack":  "content-entity:haskell_import_binding:pack",
			"src/Other/Text.hs:pack": "content-entity:haskell_import_binding:pack_decoy",
		},
	}
}

func typeScriptImportBindingCallGraphFixture() goldenCallGraphFixture {
	return goldenCallGraphFixture{
		language: "typescript_import_binding",
		files: map[string]string{
			"main.ts": `
import { helper } from "./lib";

export function caller(): number {
  return helper();
}
`,
			"lib.ts": `
export function helper(): number {
  return 1;
}
`,
		},
		caller: "caller",
		callee: "helper",
		method: codeprovenance.MethodImportBinding,
	}
}
