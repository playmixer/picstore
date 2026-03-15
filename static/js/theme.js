// Управление темной темой

const themeToggle = document.getElementById('theme-toggle');
const STORAGE_KEY = 'theme';
const DARK_THEME_CLASS = 'dark-theme';
const LIGHT_THEME = 'light';
const DARK_THEME = 'dark';

// Функция установки темы
function setTheme(theme) {
    if (theme === DARK_THEME) {
        document.body.classList.add(DARK_THEME_CLASS);
        themeToggle.textContent = '☀️'; // солнце для светлой темы
        localStorage.setItem(STORAGE_KEY, DARK_THEME);
    } else {
        document.body.classList.remove(DARK_THEME_CLASS);
        themeToggle.textContent = '🌙'; // луна для темной темы
        localStorage.setItem(STORAGE_KEY, LIGHT_THEME);
    }
}

// Функция переключения темы
function toggleTheme() {
    const currentTheme = localStorage.getItem(STORAGE_KEY) || LIGHT_THEME;
    if (currentTheme === LIGHT_THEME) {
        setTheme(DARK_THEME);
    } else {
        setTheme(LIGHT_THEME);
    }
}

// Инициализация темы при загрузке
function initTheme() {
    const savedTheme = localStorage.getItem(STORAGE_KEY);
    if (savedTheme) {
        setTheme(savedTheme);
    } else {
        // Если нет сохраненной темы, можно проверить предпочтения системы
        const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
        if (prefersDark) {
            setTheme(DARK_THEME);
        } else {
            setTheme(LIGHT_THEME);
        }
    }
}

// Назначение обработчика события
if (themeToggle) {
    themeToggle.addEventListener('click', toggleTheme);
}

// Инициализация при загрузке DOM
document.addEventListener('DOMContentLoaded', initTheme);

// Также инициализируем тему сразу, если DOM уже загружен
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initTheme);
} else {
    initTheme();
}