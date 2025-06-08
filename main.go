package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
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
	var bookingData struct {
		Name     string `json:"name"`
		Phone    string `json:"phone"`
		Date     string `json:"date"`
		Time     string `json:"time"`
		Guests   string `json:"guests"`
		Comments string `json:"comments"`
	}

	if err := json.NewDecoder(r.Body).Decode(&bookingData); err != nil {
		log.Printf("Ошибка при разборе данных: %v", err)
		http.Error(w, "Ошибка при разборе данных", http.StatusBadRequest)
		return
	}

	// Валидация данных
	if bookingData.Name == "" || bookingData.Phone == "" || bookingData.Date == "" || bookingData.Time == "" || bookingData.Guests == "" {
		log.Printf("Не заполнены обязательные поля: name=%s, phone=%s, date=%s, time=%s, guests=%s",
			bookingData.Name, bookingData.Phone, bookingData.Date, bookingData.Time, bookingData.Guests)
		http.Error(w, "Все обязательные поля должны быть заполнены", http.StatusBadRequest)
		return
	}

	// Форматируем телефон
	phone := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, bookingData.Phone)

	// Проверяем длину телефона
	if len(phone) != 11 {
		log.Printf("Неверный формат телефона: %s", bookingData.Phone)
		http.Error(w, "Неверный формат телефона (должно быть 11 цифр)", http.StatusBadRequest)
		return
	}

	// Проверяем, что телефон начинается с 7 или 8
	if phone[0] != '7' && phone[0] != '8' {
		log.Printf("Телефон должен начинаться с 7 или 8: %s", phone)
		http.Error(w, "Телефон должен начинаться с 7 или 8", http.StatusBadRequest)
		return
	}

	// Если телефон начинается с 8, заменяем на 7
	if phone[0] == '8' {
		phone = "7" + phone[1:]
	}

	// Проверяем формат даты
	_, err := time.Parse("2006-01-02", bookingData.Date)
	if err != nil {
		log.Printf("Ошибка при проверке формата даты %s: %v", bookingData.Date, err)
		http.Error(w, "Неверный формат даты (должен быть YYYY-MM-DD)", http.StatusBadRequest)
		return
	}

	// Проверяем формат времени
	timeStr := bookingData.Time
	if len(timeStr) > 5 {
		timeStr = timeStr[:5] // Берем только часы и минуты
	}
	_, err = time.Parse("15:04", timeStr)
	if err != nil {
		log.Printf("Ошибка при проверке формата времени %s: %v", bookingData.Time, err)
		http.Error(w, "Неверный формат времени (должен быть HH:MM)", http.StatusBadRequest)
		return
	}

	// Создаем объект бронирования
	booking := Booking{
		Name:     bookingData.Name,
		Phone:    phone, // Используем отформатированный телефон
		Date:     bookingData.Date,
		Time:     timeStr,
		Guests:   bookingData.Guests,
		Comments: bookingData.Comments,
		Status:   "pending",
	}

	// Проверяем, что дата не в прошлом
	bookingDate, _ := time.Parse("2006-01-02", booking.Date)
	if bookingDate.Before(time.Now().Truncate(24 * time.Hour)) {
		log.Printf("Попытка бронирования на прошедшую дату: %s", booking.Date)
		http.Error(w, "Дата бронирования не может быть в прошлом", http.StatusBadRequest)
		return
	}

	// Сохранение бронирования
	err = db.CreateBooking(&booking)
	if err != nil {
		log.Printf("Ошибка при создании бронирования: %v", err)
		if err.Error() == "на эту дату уже существует активное бронирование для данного номера телефона" {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, fmt.Sprintf("Ошибка при создании бронирования: %v", err), http.StatusInternalServerError)
		}
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

// Добавляем общую функцию для создания шаблона с функциями форматирования
func createTemplateWithFuncs(filename string) (*template.Template, error) {
	funcMap := template.FuncMap{
		"formatPhone": func(phone string) string {
			// Убираем все нецифровые символы
			digits := strings.Map(func(r rune) rune {
				if r >= '0' && r <= '9' {
					return r
				}
				return -1
			}, phone)

			if len(digits) < 11 {
				return phone // Возвращаем исходный номер, если он некорректный
			}

			// Форматируем как +7 (XXX) XXX-XX-XX
			return fmt.Sprintf("+7 (%s) %s-%s-%s",
				digits[1:4],
				digits[4:7],
				digits[7:9],
				digits[9:11])
		},
		"formatDate": func(date string) string {
			// Проверяем, что строка имеет правильный формат YYYY-MM-DD
			if len(date) != 10 || date[4] != '-' || date[7] != '-' {
				return date
			}
			// Преобразуем YYYY-MM-DD в DD.MM.YYYY
			year := date[0:4]
			month := date[5:7]
			day := date[8:10]
			return fmt.Sprintf("%s.%s.%s", day, month, year)
		},
		"formatTime": func(time string) string {
			// Возвращаем время как есть, так как оно уже в формате HH:MM
			return time
		},
	}

	return template.New("home.html").Funcs(funcMap).ParseFiles(filename)
}

func handleAdminHome(w http.ResponseWriter, r *http.Request) {
	log.Printf("Обработка запроса к /admin: метод=%s", r.Method)

	tmpl, err := createTemplateWithFuncs("templates/admin/home.html")
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
	if r.Method == "GET" {
		// Получаем параметры фильтрации
		filters := make(map[string]string)
		if date := r.URL.Query().Get("date"); date != "" {
			filters["date"] = date
		}
		if status := r.URL.Query().Get("status"); status != "" {
			filters["status"] = status
		}
		if phone := r.URL.Query().Get("phone"); phone != "" {
			filters["phone"] = phone
		}
		if name := r.URL.Query().Get("name"); name != "" {
			filters["name"] = name
		}

		// Получаем отфильтрованные бронирования
		bookings, err := db.GetFilteredBookings(filters)
		if err != nil {
			log.Printf("Ошибка при получении бронирований: %v", err)
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}

		// Логируем данные для отладки
		for _, booking := range bookings {
			log.Printf("Booking ID %d: Date='%s', Time='%s'",
				booking.ID, booking.Date, booking.Time)
		}

		// Если это AJAX-запрос, возвращаем JSON
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(bookings)
			return
		}

		// Парсим шаблон с функциями
		tmpl, err := createTemplateWithFuncs("templates/admin/home.html")
		if err != nil {
			log.Printf("Ошибка при парсинге шаблона: %v", err)
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
			return
		}

		// Передаем только список бронирований в шаблон
		if err := tmpl.Execute(w, bookings); err != nil {
			log.Printf("Ошибка при рендеринге шаблона: %v", err)
			http.Error(w, "Внутренняя ошибка сервера", http.StatusInternalServerError)
		}
	}
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
