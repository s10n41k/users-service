package token

import (
	"fmt"
	"math/rand"
	"time"
)

func GenerateToken() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano())) // Создание нового генератора случайных чисел
	token := r.Intn(100000)                              // Генерация случайного числа от 0 до 99999
	return fmt.Sprintf("%05d", token)
}
