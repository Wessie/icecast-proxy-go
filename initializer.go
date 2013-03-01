package main

import (
    "github.com/Wessie/icecast-proxy-go/server"
    "github.com/Wessie/icecast-proxy-go/config"
    "runtime/pprof"
    "log"
    "os"
)

func main() {
    // Check if we want to profile anything
    if config.CpuProfile != "" {
        f, err := os.Create(config.CpuProfile)
        if err != nil {
            log.Fatal(err)
        }
        pprof.StartCPUProfile(f)
        defer pprof.StopCPUProfile()
    }
    
    server.Initialize()
}
