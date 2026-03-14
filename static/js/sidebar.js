// Управление боковым меню
const sidebar = document.querySelector('.sidebar');
const sidebarToggle = document.querySelector('.sidebar-toggle');
const sidebarOpenButton = document.querySelector('.sidebar-open-button');
const mainContent = document.querySelector('.main-content-with-sidebar');

function toggleSidebar() {
    console.log('Toggle sidebar');
    sidebar.classList.toggle('open');
    if (mainContent) {
        mainContent.classList.toggle('collapsed');
    }
}

function openSidebar() {
    console.log('Open sidebar');
    sidebar.classList.add('open');
    if (mainContent) {
        mainContent.classList.remove('collapsed');
    }
}

function closeSidebar() {
    console.log('Close sidebar');
    sidebar.classList.remove('open');
    if (mainContent) {
        mainContent.classList.add('collapsed');
    }
}

// Работа с ключом в localStorage
const ENCRYPTION_KEY_STORAGE = 'picstore_encryption_key';

const keyInput = document.getElementById('encryptionKey');
const keyForm = document.getElementById('keyForm');
const keyStatus = document.getElementById('keyStatus');

function updateKeyStatus() {
    const key = localStorage.getItem(ENCRYPTION_KEY_STORAGE);
    if (key) {
        keyStatus.textContent = 'сохранён';
        keyStatus.style.color = '#4CAF50';
        if (keyInput) keyInput.value = key;
    } else {
        keyStatus.textContent = 'не сохранён';
        keyStatus.style.color = '#f44336';
        if (keyInput) keyInput.value = '';
    }
}

function saveKey(event) {
    event.preventDefault();
    if (!keyInput) return;
    const key = keyInput.value.trim();
    if (key) {
        localStorage.setItem(ENCRYPTION_KEY_STORAGE, key);
        updateKeyStatus();
        alert('Ключ сохранён в localStorage.');
    } else {
        alert('Введите ключ.');
    }
}

function clearKey() {
    console.log('Clear key');
    if (confirm('Вы уверены, что хотите удалить сохранённый ключ?')) {
        localStorage.removeItem(ENCRYPTION_KEY_STORAGE);
        updateKeyStatus();
        alert('Ключ удалён.');
        document.location.reload();
    }
}

// Инициализация после загрузки DOM
document.addEventListener('DOMContentLoaded', () => {
    console.log('DOM loaded, initializing sidebar');
    console.log('Sidebar element:', sidebar);
    console.log('Sidebar open button:', sidebarOpenButton);
    console.log('Sidebar toggle:', sidebarToggle);

    // Инициализация кнопки открытия
    if (sidebarOpenButton) {
        sidebarOpenButton.onclick = openSidebar;
        console.log('Open button event assigned');
    } else {
        const openButton = document.createElement('button');
        openButton.className = 'sidebar-open-button';
        openButton.innerHTML = '☰';
        openButton.setAttribute('title', 'Открыть меню');
        openButton.onclick = openSidebar;
        document.body.appendChild(openButton);
        console.log('Open button created');
    }

    if (sidebarToggle) {
        sidebarToggle.onclick = closeSidebar;
        console.log('Close button event assigned');
    }

    if (keyForm) {
        keyForm.addEventListener('submit', saveKey);
    }

    updateKeyStatus(); // также вызовет applyKeyToPage

    // Закрытие меню при клике вне его
    document.addEventListener('click', (event) => {
        if (!sidebar.contains(event.target) && !event.target.closest('.sidebar-open-button')) {
            closeSidebar();
        }
    });
});