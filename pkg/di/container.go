/*
Copyright 2024 The Spotalis Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package di provides dependency injection container functionality for the Spotalis operator.
// It uses Uber's dig framework for runtime dependency injection with a simplified wrapper.
package di

import (
	"context"
	"fmt"

	"go.uber.org/dig"
)

// Container wraps dig.Container with Spotalis-specific functionality
type Container struct {
	*dig.Container
}

// NewContainer creates a new dependency injection container using dig
func NewContainer() *Container {
	return &Container{
		Container: dig.New(),
	}
}

// ProvideValue registers a pre-created instance in the container
// Note: This method uses reflection to create a typed constructor
func (c *Container) ProvideValue(value interface{}) error {
	// For simple values, we need to create a typed constructor
	// This is a limitation of dig - it needs typed constructors
	switch v := value.(type) {
	case string:
		return c.Provide(func() string { return v })
	case int:
		return c.Provide(func() int { return v })
	case bool:
		return c.Provide(func() bool { return v })
	default:
		// For complex types, just provide them directly
		return c.Provide(func() interface{} { return value })
	}
}

// ProvideFunc registers a constructor function in the container
func (c *Container) ProvideFunc(constructor interface{}) error {
	return c.Provide(constructor)
}

// InvokeFunc invokes a function with dependency injection
func (c *Container) InvokeFunc(function interface{}) error {
	return c.Invoke(function)
}

// InvokeFuncWithContext invokes a function with dependency injection and context
func (c *Container) InvokeFuncWithContext(ctx context.Context, function interface{}) error {
	// Dig doesn't directly support context, so we need to handle it differently
	// We can provide the context as a dependency if needed
	return c.Invoke(function)
}

// MustProvide registers a constructor and panics on error (useful for initialization)
func (c *Container) MustProvide(constructor interface{}) {
	if err := c.Provide(constructor); err != nil {
		panic(fmt.Sprintf("failed to provide dependency: %v", err))
	}
}

// MustInvoke invokes a function and panics on error (useful for initialization)
func (c *Container) MustInvoke(function interface{}) {
	if err := c.Invoke(function); err != nil {
		panic(fmt.Sprintf("failed to invoke function: %v", err))
	}
}

// String returns a string representation of the container
func (c *Container) String() string {
	return fmt.Sprintf("SpotalisContainer{digContainer: %s}", c.Container.String())
}
