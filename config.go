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
	// В реальном приложении эти значения должны быть в .env файле
	return &Config{
		DBHost:     "localhost",
		DBPort:     "5432",
		DBUser:     "postgres",
		DBPassword: "postgres",
		DBName:     "dinebook",
		DBSSLMode:  "disable",

		AdminUsername: "admin",
		AdminPassword: "admin123",
	}
}
