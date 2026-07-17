package main

type Cache interface {
	Get(key string) (string, bool)
	Set(key string, val string)
}
