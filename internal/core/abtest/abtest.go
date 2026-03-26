package abtest

import (
	"crypto/sha256"
	"math/big"

	"github.com/playmixer/single-auth/pkg/logger"
	"go.uber.org/zap"
)

// Experiment представляет A/B эксперимент с несколькими вариантами.
type Experiment struct {
	Name      string
	Variants  []Variant
	Overrides map[string]string // ключ -> вариант (для принудительного назначения)
	log       *logger.Logger
}

// Variant описывает один вариант эксперимента.
type Variant struct {
	Name    string  // уникальное имя (например, "A", "B", "control")
	Percent float64 // процент трафика (0-100)
}

// NewExperiment создаёт новый эксперимент.
func NewExperiment(name string, log *logger.Logger) *Experiment {
	return &Experiment{
		Name:      name,
		Variants:  []Variant{},
		Overrides: make(map[string]string),
		log:       log,
	}
}

// AddVariant добавляет вариант с указанным процентом трафика.
// Сумма процентов всех вариантов не должна превышать 100.
func (e *Experiment) AddVariant(name string, percent float64) {
	e.Variants = append(e.Variants, Variant{Name: name, Percent: percent})
}

// SetOverride устанавливает принудительный вариант для заданного ключа (например, userID).
func (e *Experiment) SetOverride(key, variant string) {
	e.Overrides[key] = variant
}

// Assign определяет вариант для данного ключа (например, userID или sessionID).
// Возвращает имя выбранного варианта.
func (e *Experiment) Assign(key string) string {
	// Проверяем переопределение
	if v, ok := e.Overrides[key]; ok {
		e.log.Debug("experiment override", zap.String("experiment", e.Name), zap.String("key", key), zap.String("variant", v))
		return v
	}

	// Вычисляем хэш ключа для детерминированного распределения
	hash := sha256.Sum256([]byte(key))
	hashInt := new(big.Int).SetBytes(hash[:])
	// Берём остаток от деления на 10000 для более точного распределения
	mod := new(big.Int).Mod(hashInt, big.NewInt(10000))
	point := float64(mod.Int64()) / 100.0 // значение от 0.00 до 99.99

	// Распределяем по диапазонам процентов
	cumulative := 0.0
	for _, v := range e.Variants {
		cumulative += v.Percent
		if point < cumulative {
			e.log.Debug("experiment assigned", zap.String("experiment", e.Name), zap.String("key", key), zap.String("variant", v.Name), zap.Float64("point", point))
			return v.Name
		}
	}

	// Если по какой-то причине не попали ни в один вариант (сумма процентов < 100),
	// возвращаем последний вариант.
	if len(e.Variants) > 0 {
		last := e.Variants[len(e.Variants)-1].Name
		e.log.Warn("experiment fallback to last variant", zap.String("experiment", e.Name), zap.String("key", key), zap.String("variant", last))
		return last
	}

	// Если вариантов нет, возвращаем "control"
	e.log.Warn("experiment has no variants", zap.String("experiment", e.Name))
	return "control"
}

// ParseQueryOverride извлекает переопределение варианта из query строки.
// Формат: ?ab_experiment=<experiment_name>:<variant>
// Пример: ?ab_experiment=navigation_prefetch:A
// Если query содержит такой параметр, он будет использован для соответствующего эксперимента.
func ParseQueryOverride(query string) map[string]string {
	overrides := make(map[string]string)
	// Упрощённый парсинг, предполагаем что query уже разобрана (например, r.URL.Query())
	// Здесь просто демонстрация.
	return overrides
}

// Manager управляет множеством экспериментов.
type Manager struct {
	experiments map[string]*Experiment
	log         *logger.Logger
}

// NewManager создаёт новый менеджер экспериментов.
func NewManager(log *logger.Logger) *Manager {
	return &Manager{
		experiments: make(map[string]*Experiment),
		log:         log,
	}
}

// RegisterExperiment регистрирует эксперимент в менеджере.
func (m *Manager) RegisterExperiment(exp *Experiment) {
	m.experiments[exp.Name] = exp
}

// GetExperiment возвращает эксперимент по имени.
func (m *Manager) GetExperiment(name string) (*Experiment, bool) {
	exp, ok := m.experiments[name]
	return exp, ok
}

// Assign определяет вариант для эксперимента и ключа.
func (m *Manager) Assign(experimentName, key string) string {
	if exp, ok := m.experiments[experimentName]; ok {
		return exp.Assign(key)
	}
	m.log.Warn("experiment not found", zap.String("experiment", experimentName))
	return "control"
}

// Example использования:
// func main() {
//     log := logger.New()
//     exp := abtest.NewExperiment("navigation_prefetch", log)
//     exp.AddVariant("A", 50) // 50% трафика
//     exp.AddVariant("B", 50) // 50% трафика
//     manager := abtest.NewManager(log)
//     manager.RegisterExperiment(exp)
//     variant := manager.Assign("navigation_prefetch", "user123")
//     fmt.Println(variant)
// }
