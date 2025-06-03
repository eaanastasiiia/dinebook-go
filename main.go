package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type Booking struct {
	ID       int       `json:"id"`
	Name     string    `json:"name"`
	Phone    string    `json:"phone"`
	Date     string    `json:"date"`
	Time     string    `json:"time"`
	Guests   string    `json:"guests"`
	Comments string    `json:"comments"`
	Status   string    `json:"status"`
	Created  time.Time `json:"created"`
}

var db *Database

func main() {
	config := GetConfig()

	// Инициализация базы данных
	var err error
	db, err = NewDatabase(config)
	if err != nil {
		log.Fatalf("Ошибка инициализации базы данных: %v", err)
	}
	defer db.Close()

	// Создание администратора по умолчанию
	if err := db.CreateAdminUser(config.AdminUsername, config.AdminPassword); err != nil {
		log.Printf("Ошибка создания администратора: %v", err)
	}

	router := mux.NewRouter()

	// Статические файлы
	fs := http.FileServer(http.Dir("static"))
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	// Публичные маршруты
	router.HandleFunc("/", handleHome).Methods("GET")
	router.HandleFunc("/api/book", handleCreateBooking).Methods("POST")
	router.HandleFunc("/api/bookings", handleGetBookingsByPhone).Methods("GET")
	router.HandleFunc("/api/bookings/{id}/status", handleUpdateBookingStatus).Methods("PUT")

	// Административные маршруты
	adminRouter := router.PathPrefix("/admin").Subrouter()
	adminRouter.HandleFunc("/login", handleAdminLogin).Methods("GET", "POST")

	// Защищенные админ-маршруты
	protectedAdmin := adminRouter.PathPrefix("").Subrouter()
	protectedAdmin.Use(authMiddleware)
	protectedAdmin.HandleFunc("", handleAdminHome).Methods("GET")
	protectedAdmin.HandleFunc("/", handleAdminHome).Methods("GET")
	protectedAdmin.HandleFunc("/bookings", handleAdminBookings).Methods("GET")
	protectedAdmin.HandleFunc("/bookings/{id}/status", handleUpdateBookingStatus).Methods("PUT")

	log.Println("Сервер запущен на http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", router))
}

func handleHome(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func handleCreateBooking(w http.ResponseWriter, r *http.Request) {
	var booking Booking
	if err := json.NewDecoder(r.Body).Decode(&booking); err != nil {
		log.Printf("Ошибка при разборе данных: %v", err)
		http.Error(w, "Ошибка при разборе данных", http.StatusBadRequest)
		return
	}

	log.Printf("Получены данные бронирования: %+v", booking)

	// Валидация данных
	if booking.Name == "" || booking.Phone == "" || booking.Date == "" || booking.Time == "" || booking.Guests == "" {
		log.Printf("Не заполнены обязательные поля: name=%s, phone=%s, date=%s, time=%s, guests=%s",
			booking.Name, booking.Phone, booking.Date, booking.Time, booking.Guests)
		http.Error(w, "Все обязательные поля должны быть заполнены", http.StatusBadRequest)
		return
	}

	// Проверка даты
	bookingDate, err := time.Parse("2006-01-02", booking.Date)
	if err != nil {
		log.Printf("Ошибка при парсинге даты %s: %v", booking.Date, err)
		http.Error(w, "Неверный формат даты", http.StatusBadRequest)
		return
	}

	if bookingDate.Before(time.Now().Truncate(24 * time.Hour)) {
		log.Printf("Попытка бронирования на прошедшую дату: %s", booking.Date)
		http.Error(w, "Дата бронирования не может быть в прошлом", http.StatusBadRequest)
		return
	}

	// Сохранение бронирования
	if err := db.CreateBooking(&booking); err != nil {
		log.Printf("Ошибка при создании бронирования: %v", err)
		http.Error(w, fmt.Sprintf("Ошибка при создании бронирования: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Бронирование успешно создано: ID=%d", booking.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Бронирование успешно создано",
	})
}

func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Пропускаем middleware для страницы входа
		if r.URL.Path == "/admin/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Проверяем сессию
		session, err := r.Cookie("session")
		if err != nil || session.Value == "" {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	log.Printf("Обработка запроса к /admin/login: метод=%s", r.Method)

	if r.Method == "GET" {
		tmpl, err := template.ParseFiles("templates/admin/login.html")
		if err != nil {
			log.Printf("Ошибка при загрузке шаблона login.html: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := struct {
			Error string
		}{}
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Ошибка при рендеринге шаблона login.html: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")
	log.Printf("Попытка входа: username=%s", username)

	valid, err := db.ValidateAdmin(username, password)
	if err != nil {
		log.Printf("Ошибка при валидации админа: %v", err)
		http.Error(w, "Ошибка сервера при проверке учетных данных", http.StatusInternalServerError)
		return
	}
	if !valid {
		log.Printf("Неверные учетные данные для пользователя: %s", username)
		tmpl, _ := template.ParseFiles("templates/admin/login.html")
		tmpl.Execute(w, struct{ Error string }{"Неверные имя пользователя или пароль"})
		return
	}

	// Устанавливаем куки сессии
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "admin_session",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   3600,
	})

	log.Printf("Успешный вход пользователя: %s", username)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func handleAdminHome(w http.ResponseWriter, r *http.Request) {
	log.Printf("Обработка запроса к /admin: метод=%s", r.Method)

	tmpl, err := template.ParseFiles("templates/admin/home.html")
	if err != nil {
		log.Printf("Ошибка при загрузке шаблона home.html: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	bookings, err := db.GetBookings()
	if err != nil {
		log.Printf("Ошибка при получении бронирований: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, bookings); err != nil {
		log.Printf("Ошибка при рендеринге шаблона home.html: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func handleAdminBookings(w http.ResponseWriter, r *http.Request) {
	bookings, err := db.GetBookings()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookings)
}

func handleUpdateBookingStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	idStr := vars["id"]
	log.Printf("Обновление статуса бронирования: id=%s, путь=%s", idStr, r.URL.Path)

	var id int
	if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
		log.Printf("Неверный формат ID: %s", idStr)
		http.Error(w, "Неверный формат ID", http.StatusBadRequest)
		return
	}

	var data struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		log.Printf("Ошибка при разборе JSON: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Проверяем, существует ли бронирование
	_, err := db.GetBookingByID(id)
	if err != nil {
		log.Printf("Ошибка при получении бронирования %d: %v", id, err)
		http.Error(w, "Бронирование не найдено", http.StatusNotFound)
		return
	}

	log.Printf("Обновление статуса бронирования %d на %s", id, data.Status)

	// Если статус "cancelled", помечаем бронирование как отмененное
	if data.Status == "cancelled" {
		if err := db.UpdateBookingStatus(id, "cancelled"); err != nil {
			log.Printf("Ошибка при отмене бронирования: %v", err)
			http.Error(w, "Ошибка при отмене бронирования", http.StatusInternalServerError)
			return
		}
	} else {
		// Для других статусов просто обновляем статус
		if err := db.UpdateBookingStatus(id, data.Status); err != nil {
			log.Printf("Ошибка при обновлении статуса бронирования: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "Статус бронирования успешно обновлен",
	})
}

func handleGetBookingsByPhone(w http.ResponseWriter, r *http.Request) {
	phone := r.URL.Query().Get("phone")
	if phone == "" {
		http.Error(w, "Не указан номер телефона", http.StatusBadRequest)
		return
	}

	log.Printf("Поиск бронирований для телефона: %s", phone)

	bookings, err := db.GetBookingsByPhone(phone)
	if err != nil {
		log.Printf("Ошибка при поиске бронирований: %v", err)
		http.Error(w, fmt.Sprintf("Ошибка при поиске бронирований: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Найдено бронирований: %d", len(bookings))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bookings)
}
