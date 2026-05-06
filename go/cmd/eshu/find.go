package main

import (
	"github.com/spf13/cobra"
)

var findCmd = &cobra.Command{
	Use:   "find",
	Short: "Search and discover entities in the code graph",
}

func init() {
	rootCmd.AddCommand(findCmd)

	// find name
	nameCmd := &cobra.Command{
		Use:   "name <name>",
		Short: "Exact-name search",
		Args:  cobra.ExactArgs(1),
		RunE:  runFindName,
	}
	addRemoteFlags(nameCmd)
	findCmd.AddCommand(nameCmd)

	// find pattern
	patternCmd := &cobra.Command{
		Use:   "pattern <text>",
		Short: "Substring search",
		Args:  cobra.ExactArgs(1),
		RunE:  runFindPattern,
	}
	addRemoteFlags(patternCmd)
	findCmd.AddCommand(patternCmd)

	// find type
	typeCmd := &cobra.Command{
		Use:   "type <type>",
		Short: "List all nodes of one type",
		Args:  cobra.ExactArgs(1),
		RunE:  runFindType,
	}
	addRemoteFlags(typeCmd)
	findCmd.AddCommand(typeCmd)

	// find variable
	varCmd := &cobra.Command{
		Use:   "variable <name>",
		Short: "Find variables by name",
		Args:  cobra.ExactArgs(1),
		RunE:  runFindVariable,
	}
	addRemoteFlags(varCmd)
	findCmd.AddCommand(varCmd)

	// find content
	contentCmd := &cobra.Command{
		Use:   "content <text>",
		Short: "Full-text search in source content",
		Args:  cobra.ExactArgs(1),
		RunE:  runFindContent,
	}
	addRemoteFlags(contentCmd)
	findCmd.AddCommand(contentCmd)

	// find decorator
	decoratorCmd := &cobra.Command{
		Use:   "decorator <name>",
		Short: "Find functions with a decorator",
		Args:  cobra.ExactArgs(1),
		RunE:  runFindDecorator,
	}
	addRemoteFlags(decoratorCmd)
	findCmd.AddCommand(decoratorCmd)

	// find argument
	argCmd := &cobra.Command{
		Use:   "argument <name>",
		Short: "Find functions with a parameter name",
		Args:  cobra.ExactArgs(1),
		RunE:  runFindArgument,
	}
	addRemoteFlags(argCmd)
	findCmd.AddCommand(argCmd)
}

func runFindName(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/entities/resolve", map[string]any{
		"name": args[0],
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runFindPattern(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/code/search", map[string]any{
		"query": args[0],
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runFindType(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/code/search", map[string]any{
		"query": args[0],
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runFindVariable(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/code/search", map[string]any{
		"query": args[0],
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runFindContent(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/content/entities/search", map[string]any{
		"query": args[0],
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runFindDecorator(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/code/search", map[string]any{
		"query": args[0],
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}

func runFindArgument(cmd *cobra.Command, args []string) error {
	client := apiClientFromCmd(cmd)
	var result any
	err := client.Post("/api/v0/code/search", map[string]any{
		"query": args[0],
	}, &result)
	if err != nil {
		return err
	}
	printJSON(result)
	return nil
}
