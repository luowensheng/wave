package storage

import (
	"easyserver/io/http/contentloader"
	"easyserver/domain"
	"fmt"
)

type StorageConfig = domain.StorageConfig
type TableDef = domain.TableDef

type Config struct {
	Storage map[string]StorageConfig `yaml:"storage"`
}

type StorageRef interface {
	Execute(command string, data *contentloader.DataLoader) (any, error)
}

var _STORAGE_REFS map[string]StorageRef

func GetFromStorage(name string)(StorageRef, bool) {
	value, ok := _STORAGE_REFS[name]
	return value, ok
}

func InitStorage(storage map[string]*StorageConfig) error {
	var err error
	_STORAGE_REFS, err = initStorage(storage)
	return err
}

func initStorage(storage map[string]*StorageConfig) (map[string]StorageRef, error) {
	refs := map[string]StorageRef{}
	if storage == nil {
		return refs, nil
	}

	var err error
	defer fmt.Println("DONE InitStorage")

	for key, config := range storage {
		fmt.Println("PROCESSING STORAGE: ", key)

		var ref StorageRef

		switch config.Type {
		case "filesystem":
			fmt.Println("SETUP FileSystem")
			ref, err = setupFileSystem(config)
			if err != nil {
				fmt.Println("SETUP: ", err.Error())
				return nil, err
			}
		case "sqlite":
			fmt.Println("SETUP SQLITE")
			ref, err = setupSQLITE(config)
			if err != nil {
				fmt.Println("SETUP: ", err.Error())
				return nil, err
			}
		default:
			fmt.Println("SETUP ERR")
			return nil, fmt.Errorf("invalid storage type: '%s'", config.Type)
		}

		fmt.Println("DONE PROCESSING: ", key)

		refs[key] = ref
	}
	return refs, nil
}
