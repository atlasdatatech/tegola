package config

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestReplaceEnvVars(t *testing.T) {
	type TestCase struct {
		config   string
		envVars  map[string]string
		expected string
	}

	var testCases []TestCase = []TestCase{
		{
			config:   "SomeParam = $MY_ENV_VAR, SomeOtherParam = $MY_2ND_VAR",
			envVars:  map[string]string{"MY_ENV_VAR": "p1", "MY_2ND_VAR": "p2"},
			expected: "SomeParam = p1, SomeOtherParam = p2",
		},
		{
			config:   "SomeParam2 = $MY_ENV_VAR, SomeOtherParam2 = $MY_2ND_VAR",
			envVars:  map[string]string{"MY_ENV_VAR": "p2"},
			expected: "SomeParam2 = p2, SomeOtherParam2 = ",
		},
		{
			config:   "SomeParam3 = $MY_ENV_VAR, SomeOtherParam3 = $32.78",
			envVars:  map[string]string{"MY_ENV_VAR": "p3", "UNUSED_VAR": "notused"},
			expected: "SomeParam3 = p3, SomeOtherParam3 = $32.78",
		},
	}

	for i, tc := range testCases {
		var byteResult []byte
		var result string

		rdr := strings.NewReader(tc.config)
		for envVar, value := range tc.envVars {
			os.Setenv(envVar, value)
		}
		resultRdr, err := replaceEnvVars(rdr)
		if err != nil {
			t.Errorf("[%v] Error returned by call to replaceEnvVars(): %v", err)
		}

		// Unset the env vars prior to the next test.
		for envVar, _ := range tc.envVars {
			os.Unsetenv(envVar)
		}

		byteResult, _ = ioutil.ReadAll(resultRdr)
		result = string(byteResult)
		if result != tc.expected {
			t.Errorf("[%v] '%v' != '%v'", i, result, tc.expected)
		}
	}
}
