package main

import (
	"fmt"
	"net/http"
)

func main() {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	_ = http.ListenAndServe(":8080", nil)
}
