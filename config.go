package main

type Config struct {
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	AdminUsername string
	AdminPassword string
}

func GetConfig() *Config {
	// Настройки для Postgres.app по умолчанию
	return &Config{
		DBHost:     "localhost",
		DBPort:     "5432",
		DBUser:     "postgres",
		DBPassword: "postgres", // Стандартный пароль для Postgres.app
		DBName:     "dinebook",
		DBSSLMode:  "disable", // Для локальной разработки отключаем SSL

		AdminUsername: "admin",
		AdminPassword: "admin123",
	}
}
