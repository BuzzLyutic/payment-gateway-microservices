package middleware

import (
	"testing"
	"time"
)

func TestSecondsUntilNextMinute_Range(t *testing.T) {
	// secondsUntilNextMinute должна возвращать 1-61 секунду.
	// Нельзя точно предсказать значение (зависит от момента запуска),
	// но диапазон фиксирован: от 1 (конец минуты) до 61 (начало минуты).
	result := secondsUntilNextMinute()

	if result < 1 || result > 61 {
		t.Errorf("secondsUntilNextMinute() = %d, want [1, 61]", result)
	}
}

func TestSecondsUntilNextMinute_AlwaysPositive(t *testing.T) {
	// Запускаем несколько раз — результат всегда > 0.
	for i := 0; i < 10; i++ {
		result := secondsUntilNextMinute()
		if result <= 0 {
			t.Errorf("iteration %d: secondsUntilNextMinute() = %d, want > 0", i, result)
		}
	}
}

func TestSecondsUntilNextMinute_Consistency(t *testing.T) {
	// Два последовательных вызова — второй <= первого (время идёт вперёд).
	first := secondsUntilNextMinute()
	time.Sleep(time.Millisecond)
	second := secondsUntilNextMinute()

	// second <= first, за исключением перехода через минуту
	// (крайне редкий edge case в тестах).
	if second > first+1 {
		t.Errorf("second call (%d) > first call (%d) + 1, unexpected", second, first)
	}
}
