package main

import (
	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()

	app.Post("/echo", func(c *fiber.Ctx) error {
		return c.Send(c.Body())
	})

	err := app.Listen(":8080")
	if err != nil {
		panic(err)
	}
}
