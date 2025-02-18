package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Timer struct {
	Name      string
	StartTime time.Time
	Duration  time.Duration
	StopTime  *time.Time
}

var (
	bot          *tgbotapi.BotAPI
	activeTimers = make(map[int64]*Timer)
	mu           sync.Mutex
)

const logFile = "timers.log"

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Ошибка загрузки .env файла")
	}

	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		log.Fatal("Токен бота не найден")
	}

	bot, err = tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Бот запущен:", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.CallbackQuery != nil {
			handleCallback(update.CallbackQuery)
		} else if update.Message != nil {
			handleMessage(update.Message)
		}
	}
}

func handleMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID

	switch msg.Text {
	case "/start":
		sendMainMenu(chatID)
	case "/logs":
		sendLogs(chatID)
	default:
		if timer, exists := activeTimers[chatID]; exists && timer.Duration == 0 {
			handleTimerSetup(chatID, msg.Text)
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, "Используй кнопки для работы с таймерами."))
		}
	}
}

func sendMainMenu(chatID int64) {
	msg := tgbotapi.NewMessage(chatID, "Выберите действие:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Старт таймера", "start_timer"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Показать логи", "show_logs"),
		),
	)
	bot.Send(msg)
}

func handleCallback(query *tgbotapi.CallbackQuery) {
	chatID := query.Message.Chat.ID
	data := query.Data

	switch data {
	case "start_timer":
		bot.Send(tgbotapi.NewMessage(chatID, "Введите название таймера:"))
		activeTimers[chatID] = &Timer{}
	case "show_logs":
		sendLogs(chatID)
	case "confirm_timer":
		startTimer(chatID)
	case "stop_timer":
		stopTimer(chatID, false)
	}
}

func handleTimerSetup(chatID int64, input string) {
	timer := activeTimers[chatID]

	if timer.Name == "" {
		timer.Name = input
		bot.Send(tgbotapi.NewMessage(chatID, "Введите время в минутах:"))
		return
	}

	duration, err := time.ParseDuration(input + "m")
	if err != nil {
		bot.Send(tgbotapi.NewMessage(chatID, "Введите корректное время в минутах."))
		return
	}

	timer.Duration = duration
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Запустить таймер \"%s\" на %v минут?", timer.Name, duration.Minutes()))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Запустить", "confirm_timer"),
		),
	)
	bot.Send(msg)
}

func startTimer(chatID int64) {
	mu.Lock()
	timer, exists := activeTimers[chatID]
	mu.Unlock()
	if !exists || timer.Name == "" || timer.Duration == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "Ошибка! Сначала настройте таймер."))
		return
	}

	timer.StartTime = time.Now()
	logTimer(timer, "Запуск")

	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Таймер \"%s\" запущен на %v минут.", timer.Name, timer.Duration.Minutes()))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Остановить таймер", "stop_timer"),
		),
	)
	bot.Send(msg)

	go func(chatID int64, duration time.Duration) {
		time.Sleep(duration)
		mu.Lock()
		_, exists := activeTimers[chatID]
		mu.Unlock()
		if exists {
			stopTimer(chatID, true)
		}
	}(chatID, timer.Duration)
}

func stopTimer(chatID int64, auto bool) {
	mu.Lock()
	timer, exists := activeTimers[chatID]
	if !exists {
		mu.Unlock()
		bot.Send(tgbotapi.NewMessage(chatID, "Нет активного таймера."))
		return
	}

	stopTime := time.Now()
	timer.StopTime = &stopTime
	logTimer(timer, "Остановлен")
	delete(activeTimers, chatID)
	mu.Unlock()

	message := fmt.Sprintf("Таймер \"%s\" остановлен.", timer.Name)
	if auto {
		message += " ⏳ Время истекло!"
	}
	bot.Send(tgbotapi.NewMessage(chatID, message))
}

func logTimer(timer *Timer, action string) {
	entry := fmt.Sprintf("%s | Название: %s | Начало: %s | Окончание: %s\n",
		action,
		timer.Name,
		timer.StartTime.Format("2006-01-02 15:04:05"),
		func() string {
			if timer.StopTime != nil {
				return timer.StopTime.Format("2006-01-02 15:04:05")
			}
			return "В процессе"
		}(),
	)

	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Println("Ошибка записи в лог:", err)
		return
	}
	defer file.Close()

	_, err = file.WriteString(entry)
	if err != nil {
		log.Println("Ошибка при сохранении логов:", err)
	}
}

func sendLogs(chatID int64) {
	data, err := os.ReadFile(logFile)
	if err != nil || len(data) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "🔍 Логи пусты."))
		return
	}

	bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("📜 Логи таймеров:\n\n%s", string(data))))
}


func splitLines(s string) []string {
	var lines []string
	for _, line := range splitBy(s, '\n') {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitBy(s string, sep rune) []string {
	var res []string
	for _, part := range s {
		res = append(res, string(part))
	}
	return res
}

