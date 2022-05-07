package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"

	"vendor.lib/tng/tng-lib/db/mgo"
	"vendor.lib/tng/tng-lib/db/sql"
	"vendor.lib/tng/tng-lib/http"

	"github.com/rs/zerolog/log"

	"github.com/pkg/errors"
)

// Application ...
type Application struct {
	Name  string `json:"name"`
	Port  int    `json:"port"`
	Debug bool   `json:"debug"`

	LogLevel string `json:"log-level"`

	Client  http.Config            `json:"client"`
	Clients map[string]http.Config `json:"clients"`

	Collection  string            `json:"collection"`
	Collections map[string]string `json:"collections"`
}

// Datasource represents the basic configurations for the mongo and sql databases.
type Datasource struct {
	Mongo map[string]mgo.Config `json:"mongo"`
	SQL   sql.Config            `json:"sql"`
}

// Links represents links key map values used to make http client calls.
type Links map[string]string

// Read opens and reads a file into config struct based on path location.
func Read(path string, conf interface{}) error {
	if reflect.ValueOf(conf).Kind() != reflect.Ptr {
		return errors.New("interface is not a pointer")
	}

	file, err := os.Open(path)
	if err != nil {
		log.Error().Stack().Caller().Err(err).Msgf("unable to open file: %s", path)
		return err
	}
	defer file.Close()

	err = json.NewDecoder(file).Decode(conf)
	if err != nil {
		log.Error().Stack().Caller().Err(err).Msgf("unable to parse file: %s", path)
		return err
	}

	if err := translate(reflect.TypeOf(conf), reflect.ValueOf(conf)); err != nil {
		log.Error().Stack().Caller().Err(err).Msgf("unable to translate file: %s", path)
		return err
	}

	return nil
}

// translate determines which type and value is being passed in and recursively unwraps each field/value within that type.
// reference: https:// gist.github.com/hvoecking/10772475
func translate(reflectType reflect.Type, reflectValue reflect.Value) error {
	switch reflectValue.Kind() {
	// The first cases handle nested structures and translates them recursively. If it is a pointer we need to unwrap and
	// call once again
	case reflect.Ptr:
		pointerValue := reflectValue.Elem()
		if !pointerValue.IsValid() {
			return errors.New("nil pointer value")
		}

		// Unwrap the newly created pointer
		if err := translate(pointerValue.Type(), pointerValue); err != nil {
			return err
		}

	// If it is an interface (which is very similar to a pointer), do basically the same as for the pointer. Though a
	// pointer is not the same as an interface so note that we have to call Elem() after creating a new object otherwise
	// we would end up with an actual pointer
	case reflect.Interface:
		interfaceValue := reflectValue.Elem()

		// Create a new object (of type pointer)and retrieve the value it points to
		copyValue := reflect.New(interfaceValue.Type()).Elem()

		if err := translate(interfaceValue.Type(), interfaceValue); err != nil {
			return err
		}

		reflectValue.Set(copyValue)

	// If it is a struct we translate each field. We retrieve the number of fields within the struct and iterate through
	// each field. For each 'base64' lookup tag, we will retrieve that fields key map values and base64 decode the value
	// and update the value with the decoded value.
	case reflect.Struct:
		for i := 0; i < reflectType.NumField(); i++ {
			if _, ok := reflectType.Field(i).Tag.Lookup("base64"); ok {
				structField := reflectValue.Field(i)
				original := structField.String()
				if original != "" {
					decoded, err := base64.StdEncoding.DecodeString(original)
					if err != nil {
						return err
					}

					structField.SetString(string(decoded))
				}
			} else {
				if err := translate(reflectValue.Field(i).Type(), reflectValue.Field(i)); err != nil {
					return err
				}
			}
		}

	// If it is a slice translate each element
	case reflect.Slice:
		for i := 0; i < reflectValue.Len(); i++ {
			if err := translate(reflectValue.Index(i).Type(), reflectValue.Index(i)); err != nil {
				return err
			}
		}

	// If it is a map we create a new map and translate each value
	case reflect.Map:
		for _, key := range reflectValue.MapKeys() {
			if reflectValue.MapIndex(key).Kind() == reflect.Struct {
				original := reflectValue.MapIndex(key)
				copyValue := reflect.New(original.Type()).Elem()
				copyValue.Set(original)

				// New gives us a pointer, but again we want the value
				if err := translate(original.Type(), copyValue); err != nil {
					return err
				}

				reflectValue.SetMapIndex(key, copyValue)
			}
		}
	}

	return nil
}
