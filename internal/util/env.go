package util

import "os"

func SetEnvs(envMap map[string]string) (func(), error) {
	oldEnv := make(map[string]string)
	restore := func() {
		for k, v := range oldEnv {
			_ = os.Setenv(k, v)
		}
		for k := range envMap {
			if _, existed := oldEnv[k]; !existed {
				_ = os.Unsetenv(k)
			}
		}
	}
	for k, v := range envMap {
		old, exist := os.LookupEnv(k)
		if exist {
			oldEnv[k] = old
		}
		if err := os.Setenv(k, v); err != nil {
			restore()
			return nil, err
		}
	}
	return restore, nil
}
