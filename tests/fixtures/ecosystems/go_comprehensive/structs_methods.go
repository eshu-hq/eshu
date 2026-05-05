package comprehensive

import "fmt"

// Point represents a 2D point.
type Point struct {
	X, Y float64
}

// Distance calculates distance to another point (value receiver).
func (p Point) Distance(other Point) float64 {
	dx := p.X - other.X
	dy := p.Y - other.Y
	return dx*dx + dy*dy
}

// Translate moves the point (pointer receiver).
func (p *Point) Translate(dx, dy float64) {
	p.X += dx
	p.Y += dy
}

// String implements the Stringer interface.
func (p Point) String() string {
	return fmt.Sprintf("(%g, %g)", p.X, p.Y)
}

// Person represents a person with embedded Address.
type Address struct {
	Street string
	City   string
}

type Person struct {
	Name string
	Age  int
	Address
}

// Greet generates a greeting for the person.
func (p *Person) Greet() string {
	return fmt.Sprintf("Hi, I'm %s from %s", p.Name, p.City)
}

// Config holds application configuration.
type Config struct {
	Host    string
	Port    int
	Debug   bool
	Options map[string]string
}

// NewConfig creates a new Config with defaults.
func NewConfig() *Config {
	return &Config{
		Host:    "localhost",
		Port:    8080,
		Options: make(map[string]string),
	}
}
