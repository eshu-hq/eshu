package main

import "fmt"

func removedCommandError(command, guidance string) error {
	printError(fmt.Sprintf("%q has been removed from the supported Go CLI contract.", command))
	if guidance != "" {
		fmt.Println(guidance)
	}
	return fmt.Errorf("%s removed from supported Go CLI contract", command)
}
