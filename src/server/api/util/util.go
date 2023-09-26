package util

import (
	"context"
	"log"
	"math"
	"time"
)

type Logger struct {
	Debug *log.Logger
	Info  *log.Logger
	Warn  *log.Logger
	Error *log.Logger
	Fatal *log.Logger
}

type ExpBackoffContext struct {
	ctx     context.Context
	base    time.Duration
	mult    float64
	retries uint
}

func NewExpBackoffContext(
	context context.Context,
	base_time time.Duration,
	multiplier float64) *ExpBackoffContext {

	return &ExpBackoffContext{
		ctx:     context,
		base:    base_time,
		mult:    multiplier,
		retries: 0,
	}
}

func (c *ExpBackoffContext) Next() (context.Context, context.CancelFunc) {
	timeout := c.base.Seconds() * math.Pow(c.mult, float64(c.retries))
	ctx, cancel := context.WithTimeout(c.ctx, time.Duration(timeout*float64(time.Second)))
	c.retries++
	return ctx, cancel
}

func (c *ExpBackoffContext) Reset() {
	c.retries = 0
}

func HasDuplicates[T comparable](values []T) bool {
	visited := make(map[T]bool, len(values))
	for _, val := range values {
		if visited[val] {
			return true
		}
		visited[val] = true
	}
	return false
}
