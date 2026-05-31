package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sort"

	"github.com/jamietre/reveillm/internal/adapter"
	"github.com/jamietre/reveillm/internal/config"
	"github.com/jamietre/reveillm/internal/hook"
	"github.com/jamietre/reveillm/internal/proxy"
	"github.com/jamietre/reveillm/internal/runner"
)

func main() {
	configPath := flag.String("config", "/etc/reveillm/config.yaml", "path to config file")
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	handler, err := buildHandler(*configPath)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("reveillm listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}

func buildHandler(configPath string) (http.Handler, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	names := make([]string, 0, len(cfg.Configs))
	for n := range cfg.Configs {
		names = append(names, n)
	}
	sort.Strings(names)

	a := adapter.NewOpenAIAdapter(&http.Client{})
	hooks := hook.NewManager()
	r := runner.New(cfg, a, hooks)
	return proxy.NewHandler(r, names), nil
}
