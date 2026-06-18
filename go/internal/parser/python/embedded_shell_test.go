package python

import "testing"

func TestEmbeddedShellCommandsIgnoreCommentsAndStrings(t *testing.T) {
	t.Parallel()

	source := "" +
		"import subprocess\n" +
		"\n" +
		"def documented():\n" +
		"    # subprocess.run(['rm', '-rf', '/tmp/nope'])\n" +
		"    note = \"subprocess.run(['rm', '-rf', '/tmp/nope'])\"\n" +
		"    return note\n" +
		"\n" +
		"def real(cmd):\n" +
		"    return subprocess.run(cmd)\n"

	commands := embeddedShellCommands(source)
	if len(commands) != 1 {
		t.Fatalf("embeddedShellCommands() = %#v, want one real call", commands)
	}
	if commands[0].functionName != "real" {
		t.Fatalf("functionName = %q, want real", commands[0].functionName)
	}
	if got, want := commands[0].lineNumber, 9; got != want {
		t.Fatalf("lineNumber = %d, want %d", got, want)
	}
}
