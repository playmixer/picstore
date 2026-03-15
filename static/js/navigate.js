document.addEventListener('DOMContentLoaded', function() {
    const toggleButton = document.getElementById('nav-toggle');
    const navMenu = document.getElementById('nav-menu');

    if (toggleButton && navMenu) {
        toggleButton.addEventListener('click', function() {
            navMenu.classList.toggle('active');
            toggleButton.classList.toggle('active');
        });

        // Закрытие меню при клике вне его (опционально)
        document.addEventListener('click', function(event) {
            if (!toggleButton.contains(event.target) && !navMenu.contains(event.target)) {
                navMenu.classList.remove('active');
                toggleButton.classList.remove('active');
            }
        });
    }
});