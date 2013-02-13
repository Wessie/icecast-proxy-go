package config

import (
    "github.com/kylelemons/go-gypsy/yaml"
    "flag"
)

var Config *yaml.File
var configFile string

func init() {
    flag.StringVar(&configFile, "c", "proxy.yaml", "Configuration file path.")
    flag.Parse()
    Config = yaml.ConfigFile(configFile)
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