package main

import (
	"database/sql"
	"fmt"
	"log"
	"strconv"

	_ "github.com/lib/pq"
)

type Database struct {
	*sql.DB
}

func NewDatabase(config *Config) (*Database, error) {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		config.DBHost, config.DBPort, config.DBUser, config.DBPassword, config.DBName, config.DBSSLMode)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("ошибка подключения к базе данных: %v", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ошибка проверки подключения к базе данных: %v", err)
	}

	// Создаем таблицы, если они не существуют
	if err := createTables(db); err != nil {
		return nil, err
	}

	return &Database{db}, nil
}

func createTables(db *sql.DB) error {
	// Создаем таблицу пользователей
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			username VARCHAR(50) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			is_admin BOOLEAN DEFAULT false,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("ошибка создания таблицы users: %v", err)
	}

	// Удаляем существующую таблицу bookings, если она есть
	_, err = db.Exec(`DROP TABLE IF EXISTS bookings CASCADE`)
	if err != nil {
		return fmt.Errorf("ошибка удаления таблицы bookings: %v", err)
	}

	// Создаем таблицу бронирований заново
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS bookings (
			id SERIAL PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			phone VARCHAR(20) NOT NULL,
			booking_date VARCHAR(10) NOT NULL,
			booking_time VARCHAR(5) NOT NULL,
			guests INTEGER NOT NULL,
			comments TEXT,
			status VARCHAR(20) DEFAULT 'pending',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(phone, booking_date)
		)
	`)
	if err != nil {
		return fmt.Errorf("ошибка создания таблицы bookings: %v", err)
	}

	// Создаем индексы
	_, err = db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_bookings_date ON bookings(booking_date);
		CREATE INDEX IF NOT EXISTS idx_bookings_status ON bookings(status);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_bookings_phone_date ON bookings(phone, booking_date);
	`)
	if err != nil {
		return fmt.Errorf("ошибка создания индексов: %v", err)
	}

	return nil
}

func (db *Database) CreateBooking(booking *Booking) error {
	// Проверяем существующее бронирование
	exists, err := db.CheckExistingBooking(booking.Phone, booking.Date)
	if err != nil {
		return fmt.Errorf("ошибка при проверке существующего бронирования: %v", err)
	}
	if exists {
		return fmt.Errorf("на эту дату уже существует активное бронирование для данного номера телефона")
	}

	// Преобразуем guests в число
	guests, err := strconv.Atoi(booking.Guests)
	if err != nil {
		return fmt.Errorf("неверное количество гостей: %v", err)
	}

	unique, err := db.CheckPhoneNameUnique(booking.Phone, booking.Name)
	if err != nil {
		return fmt.Errorf("ошибка проверки уникальности: %v", err)
	}
	if !unique {
		return fmt.Errorf("этот номер уже зарегистрирован на другое имя")
	}

	query := `
		INSERT INTO bookings (name, phone, booking_date, booking_time, guests, comments)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`
	return db.QueryRow(
		query,
		booking.Name,
		booking.Phone,
		booking.Date,
		booking.Time,
		guests,
		booking.Comments,
	).Scan(&booking.ID)
}

func (db *Database) GetBookings() ([]Booking, error) {
	query := `
		SELECT id, name, phone, booking_date, booking_time, guests, comments, status, created_at
		FROM bookings
		ORDER BY booking_date DESC, booking_time DESC
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bookings []Booking
	for rows.Next() {
		var b Booking
		err := rows.Scan(
			&b.ID,
			&b.Name,
			&b.Phone,
			&b.Date,
			&b.Time,
			&b.Guests,
			&b.Comments,
			&b.Status,
			&b.Created,
		)
		if err != nil {
			return nil, err
		}
		bookings = append(bookings, b)
	}
	return bookings, nil
}

func (db *Database) UpdateBookingStatus(id int, status string) error {
	log.Printf("Обновление статуса бронирования: ID=%d, новый статус=%s", id, status)

	// Сначала проверяем существование бронирования
	_, err := db.GetBookingByID(id)
	if err != nil {
		log.Printf("Ошибка при проверке существования бронирования %d: %v", id, err)
		return err
	}

	query := `
		UPDATE bookings
		SET status = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		RETURNING id
	`
	var updatedID int
	err = db.QueryRow(query, status, id).Scan(&updatedID)
	if err != nil {
		log.Printf("Ошибка при обновлении статуса бронирования %d: %v", id, err)
		return err
	}

	log.Printf("Статус бронирования успешно обновлен: ID=%d, Status=%s", updatedID, status)
	return nil
}

func (db *Database) CreateAdminUser(username, password string) error {
	// В реальном приложении пароль должен быть хэширован
	query := `
		INSERT INTO users (username, password_hash, is_admin)
		VALUES ($1, $2, true)
		ON CONFLICT (username) DO NOTHING
	`
	_, err := db.Exec(query, username, password)
	return err
}

func (db *Database) ValidateAdmin(username, password string) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM users
			WHERE username = $1 AND password_hash = $2 AND is_admin = true
		)
	`
	err := db.QueryRow(query, username, password).Scan(&exists)
	return exists, err
}

func (db *Database) GetBookingsByPhone(phone string) ([]Booking, error) {
	query := `
		SELECT id, name, phone, booking_date, booking_time, guests, comments, status, created_at
		FROM bookings
		WHERE phone = $1
		ORDER BY booking_date DESC, booking_time DESC
	`
	rows, err := db.Query(query, phone)
	if err != nil {
		return nil, fmt.Errorf("ошибка при поиске бронирований: %v", err)
	}
	defer rows.Close()

	var bookings []Booking
	for rows.Next() {
		var b Booking
		err := rows.Scan(
			&b.ID,
			&b.Name,
			&b.Phone,
			&b.Date,
			&b.Time,
			&b.Guests,
			&b.Comments,
			&b.Status,
			&b.Created,
		)
		if err != nil {
			return nil, fmt.Errorf("ошибка при чтении бронирования: %v", err)
		}
		bookings = append(bookings, b)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации по результатам: %v", err)
	}
	return bookings, nil
}

func (db *Database) GetBookingByID(id int) (*Booking, error) {
	var booking Booking
	log.Printf("Получение бронирования по ID: %d", id)

	err := db.QueryRow(`
		SELECT id, name, phone, booking_date, booking_time, guests, comments, status, created_at 
		FROM bookings 
		WHERE id = $1
	`, id).Scan(
		&booking.ID,
		&booking.Name,
		&booking.Phone,
		&booking.Date,
		&booking.Time,
		&booking.Guests,
		&booking.Comments,
		&booking.Status,
		&booking.Created,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("Бронирование с ID %d не найдено", id)
			return nil, fmt.Errorf("бронирование не найдено")
		}
		log.Printf("Ошибка при получении бронирования %d: %v", id, err)
		return nil, err
	}

	log.Printf("Успешно получено бронирование: ID=%d, Status=%s", booking.ID, booking.Status)
	return &booking, nil
}

func (db *Database) GetFilteredBookings(filters map[string]string) ([]Booking, error) {
	// Базовый запрос
	query := `
		SELECT id, name, phone, booking_date, booking_time, guests, comments, status, created_at
		FROM bookings
		WHERE 1=1
	`
	args := []interface{}{}
	argCount := 1

	// Добавляем фильтры
	if date := filters["date"]; date != "" {
		query += fmt.Sprintf(" AND booking_date = $%d", argCount)
		args = append(args, date)
		argCount++
	}
	if status := filters["status"]; status != "" {
		query += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, status)
		argCount++
	}
	if phone := filters["phone"]; phone != "" {
		query += fmt.Sprintf(" AND phone LIKE $%d", argCount)
		args = append(args, "%"+phone+"%")
		argCount++
	}
	if name := filters["name"]; name != "" {
		query += fmt.Sprintf(" AND name ILIKE $%d", argCount)
		args = append(args, "%"+name+"%")
		argCount++
	}

	// Добавляем сортировку
	query += " ORDER BY booking_date DESC, booking_time DESC"

	// Выполняем запрос
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("ошибка при получении отфильтрованных бронирований: %v", err)
	}
	defer rows.Close()

	var bookings []Booking
	for rows.Next() {
		var b Booking
		err := rows.Scan(
			&b.ID,
			&b.Name,
			&b.Phone,
			&b.Date,
			&b.Time,
			&b.Guests,
			&b.Comments,
			&b.Status,
			&b.Created,
		)
		if err != nil {
			return nil, fmt.Errorf("ошибка при чтении бронирования: %v", err)
		}
		bookings = append(bookings, b)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при итерации по результатам: %v", err)
	}

	return bookings, nil
}

func (db *Database) CheckExistingBooking(phone, date string) (bool, error) {
	var exists bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM bookings
			WHERE phone = $1 AND booking_date = $2 AND status != 'cancelled'
		)
	`
	err := db.QueryRow(query, phone, date).Scan(&exists)
	return exists, err
}

func (db *Database) CheckPhoneNameUnique(phone, name string) (bool, error) {
	var existingName string
	query := `SELECT name FROM bookings WHERE phone = $1 LIMIT 1`
	err := db.QueryRow(query, phone).Scan(&existingName)
	if err == sql.ErrNoRows {
		return true, nil // Нет такого номера — можно добавлять
	}
	if err != nil {
		return false, err
	}
	return existingName == name, nil // true — имя совпадает, false — другое имя
}
