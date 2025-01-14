package app

import (
	"courseLanding/internal/service"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
)

type Application struct {
	CounterService    service.CounterService
	PaymentService    service.PaymentService
	RepositoryService service.RepositoryService
	CourseService     service.CourseService
}

type Webhook struct {
	Type   string  `json:"type"`
	Event  string  `json:"event"`
	Object Payment `json:"object"`
}

// Payment defines the object properties
type Payment struct {
	Amount   Amount   `json:"amount"`
	Metadata Metadata `json:"metadata"`
}

// Amount defines the value and currency
type Amount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

// Metadata contains the email
type Metadata struct {
	Email string `json:"email"`
}

func (a *Application) BuyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, sec-ch-ua, sec-ch-ua-mobile, sec-ch-ua-platform")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}
	//w.Header().Set("Access-Control-Allow-Origin", "https://www.trabun.ai")

	type RequestParams struct {
		RateID int    `json:"rate"`
		Name   string `json:"name"`
		Email  string `json:"email"`
		Phone  string `json:"phone"`
		Admin  string `json:"admin"`
	}

	var params RequestParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var url string
	var id string

	phone := convertPhoneNumber(params.Phone)

	rate, err := a.RepositoryService.GetRateByID(params.RateID)
	if err != nil {
		http.Error(w, "RateID is not found"+err.Error(), http.StatusBadRequest)
	}

	status, err := ReadBoolFromFile("status.txt")
	if err != nil {
		http.Error(w, "Error during getting status", http.StatusMethodNotAllowed)
	}

	if status == false {
		http.Error(w, "Ended", http.StatusMethodNotAllowed)
		return
	}

	if (rate.Clicks > rate.Limit) && params.Admin == "" {
		http.Error(w, "Sold out", http.StatusMethodNotAllowed)
		return
	}

	url, id, err = a.PaymentService.MakePayment(rate.Price, params.Name, params.Email, phone)
	if err != nil {
		http.Error(w, "Problems with ukassa"+err.Error(), http.StatusBadRequest)
		return
	}
	err = a.RepositoryService.IncrementClicks(rate.RateID)
	if err != nil {
		http.Error(w, "Problems with incrementing", http.StatusBadRequest)
		return
	}

	// end
	fmt.Println("New order id: "+id, " email:"+params.Email, " url:"+url)
	err = insertOrder(id, params.Email)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(w, url) // здесь воозвращаем УРЛ просто текстом
}

func (a *Application) StatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	status, err := ReadBoolFromFile("status.txt")

	var statuses map[int]string
	var resp []byte
	statuses = make(map[int]string)

	rates, err := a.RepositoryService.GetAllRates()
	if err != nil {
		http.Error(w, "error during getting rates", 406)
	}

	for _, rate := range rates {
		if !status {
			statuses[rate.RateID] = "off"
			continue
		}
		if rate.Clicks >= rate.Limit {
			statuses[rate.RateID] = "stop"
		} else {
			statuses[rate.RateID] = "start"
		}
	}

	resp, err = json.Marshal(statuses)
	w.Write(resp)
	if err != nil {
		fmt.Println("Error while getting statuses")
	}
}

func (a *Application) LimitHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, sec-ch-ua, sec-ch-ua-mobile, sec-ch-ua-platform")

	// Parse the query parameter 'count'
	countStr := r.URL.Query().Get("count")
	if countStr == "" {
		fmt.Fprintf(w, "Count parameter is missing")
		return
	}

	rate := r.URL.Query().Get("rate")
	if countStr == "" {
		fmt.Fprintf(w, "Count parameter is missing")
		return
	}
	rateForDb, err := strconv.Atoi(rate)

	// Convert count to an integer
	count, err := strconv.Atoi(countStr)
	if err != nil {
		fmt.Fprintf(w, "Invalid count value")
		return
	}

	a.RepositoryService.UpdateLimit(rateForDb, count)
	// Print the count value
	fmt.Println("Count:", count)
}

func (a *Application) EnableHandler(w http.ResponseWriter, r *http.Request) {
	value := FlipBoolInFile("status.txt")

	w.Write([]byte(value))
}

// code standarts ignored:

func ReadBoolFromFile(filename string) (bool, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return false, err
	}

	return string(data) == "1", nil
}

// FlipBoolInFile flips the boolean value in the file.
func FlipBoolInFile(filename string) string {
	value, _ := ReadBoolFromFile(filename)

	newValue := "0"
	if !value {
		newValue = "1"
	}

	ioutil.WriteFile(filename, []byte(newValue), 0666)

	return newValue
}

func insertOrder(id string, email string) error {
	// Open SQLite database
	db, err := sql.Open("sqlite3", "./orders.db")
	if err != nil {
		return err
	}
	defer db.Close()

	// Prepare statement to insert data into "orders" table
	stmt, err := db.Prepare("INSERT INTO orders(payment_id, email) VALUES(?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Execute the statement with provided id and email
	_, err = stmt.Exec(id, email)
	if err != nil {
		return err
	}

	fmt.Println("Inserted order:", id, email)
	return nil
}

func convertPhoneNumber(phoneNumber string) string {
	if strings.HasPrefix(phoneNumber, "+7") {
		return phoneNumber[1:]
	} else if strings.HasPrefix(phoneNumber, "8") {
		return "7" + phoneNumber[1:]
	}
	return phoneNumber
}
