package config

import (
    "github.com/kylelemons/go-gypsy/yaml"
    "flag"
    "time"
)

const BUFFER_SIZE = 4096
const QUEUE_LIMIT = 100
const Timeout = time.Second * 5

var Config *yaml.File
var configFile string
var AuthFile string
var Authentication bool = true
var ServerAddress = ":"
var CpuProfile string
var MemoryProfile string


func init() {
    flag.StringVar(&configFile, "c", "proxy.yaml", "Configuration file path.")
    flag.StringVar(&AuthFile, "ac", "auth.txt", "Authentication file path.")
    flag.BoolVar(&Authentication, "auth", true, "False if authentication should be disabled")
    flag.StringVar(&CpuProfile, "cpuprofile", "", "Write CPU profile to file")
    flag.StringVar(&MemoryProfile, "memoryprofile", "", "Write Memory profile to file")
    flag.Parse()
    Config = yaml.ConfigFile(configFile)
    
    node, err := yaml.Child(Config.Root, "server")
    if err != nil {
        panic("Server configuration missing.")
    }
    
    if m, ok := node.(yaml.Map); ok {
        for key, value := range m {
            if scalar, ok := value.(yaml.Scalar); ok {
                if key == "host" {
                    ServerAddress = string(scalar) + ServerAddress
                } else if key == "port" {
                    ServerAddress = ServerAddress + string(scalar)
                }
            }
        }
    }
}

func CreateShoutMap() map[string] string {
    /* Utility function that returns a mapping that can be used
    to pass to shout.NewShout */
    node, err := yaml.Child(Config.Root, "icecast")
    if err != nil {
        panic("Icecast configuration missing.")
    }

    icecast_map := make(map[string] string, 20)
    
    if m, ok := node.(yaml.Map); ok {
        for key, value := range m {
            if scalar, ok := value.(yaml.Scalar); ok {
                icecast_map[key] = string(scalar)
            }
        }
    } else {
        panic("Icecast configuration isn't a mapping.")
    }
    return icecast_map
}

func CreateDatabaseDSN() string {
    /*
    An utility function that returns a new DSN string. This is of the format
    
    [username[:password]@][protocol[(address)]]/dbname[?param1=value1&paramN=valueN]
    
    it is generated from the "database" header in the configuration file.
    
    The result can be passed to sql.Open for mysql usage.
    */
    node, err := yaml.Child(Config.Root, "database")
    if err != nil {
        panic("Database configuration missing.")
    }
    
    DBN := ""

    if m, ok := node.(yaml.Map); ok {
        getstring := func (key string) (string, bool) {
            if scalar, ok := m[key].(yaml.Scalar); ok {
                return string(scalar), true
            }
            return "", false
        }
        if name, ok := getstring("username"); ok {
            DBN += name
        }
        if pass, ok := getstring("password"); ok {
            DBN += ":" + pass
        }
        if protocol, ok := getstring("protocol"); ok {
            DBN += "@" + protocol
        }
        if address, ok := getstring("host"); ok {
            if port, ok := getstring("port"); ok {
                address += ":" + port
            }
            DBN += "(" + address + ")"
        }
        if dbname, ok := getstring("dbname"); ok {
            DBN += "/" + dbname
        } else {
            panic("Database name is required.")
        }
        params := m["parameters"]
        if mp, ok := params.(yaml.Map); ok {
            i := 0
            DBN += "?"
            for key, value := range mp {
                // Add separators if we have more than one parameter
                if i++; i > 1 { DBN += "&" }
                
                if scalar, ok := value.(yaml.Scalar); ok {
                    DBN += key + "=" + string(scalar)
                } else {
                    
                }
            }
        }
    }
    return DBN
}