/**
 * Fullscreen mode for image viewing
 */
(function() {
    'use strict';

    // Wait for DOM to be fully loaded
    document.addEventListener('DOMContentLoaded', function() {
        const container = document.querySelector('.view-image-container');
        const image = document.querySelector('.view-image');
        const button = document.querySelector('.fullscreen-button');

        if (!container || !image || !button) {
            // Elements not found (maybe encrypted placeholder)
            return;
        }

        // Toggle fullscreen
        function toggleFullscreen() {
            container.classList.toggle('fullscreen');
            if (container.classList.contains('fullscreen')) {
                button.textContent = '✕';
                button.setAttribute('title', 'Выйти из полноэкранного режима');
                // Prevent body scroll
                document.body.style.overflow = 'hidden';
            } else {
                button.textContent = '⛶';
                button.setAttribute('title', 'Полноэкранный режим');
                document.body.style.overflow = '';
            }
        }

        // Click on button
        button.addEventListener('click', function(e) {
            e.stopPropagation();
            toggleFullscreen();
        });

        // Click on image to toggle (optional)
        image.addEventListener('click', function(e) {
            if (e.target === image) {
                toggleFullscreen();
            }
        });

        // Exit fullscreen on Escape key
        document.addEventListener('keydown', function(e) {
            if (e.key === 'Escape' && container.classList.contains('fullscreen')) {
                toggleFullscreen();
            }
        });

        // Automatically enter fullscreen when navigating to next/prev image
        // This will be triggered by the navigation script
        window.enterFullscreenOnLoad = function() {
            // Check if we should auto-enter fullscreen based on user preference?
            // For now, we can store a flag in sessionStorage
            if (sessionStorage.getItem('autoFullscreen') === 'true') {
                if (!container.classList.contains('fullscreen')) {
                    toggleFullscreen();
                }
                // Clear flag after using
                sessionStorage.setItem('autoFullscreen', 'false');
            }
        };

        // Set flag when leaving page (if currently in fullscreen)
        window.addEventListener('beforeunload', function() {
            if (container.classList.contains('fullscreen')) {
                sessionStorage.setItem('autoFullscreen', 'true');
            } else {
                sessionStorage.setItem('autoFullscreen', 'false');
            }
        });

        // Call auto-enter on load
        enterFullscreenOnLoad();

        // Log for debugging
        console.log('Fullscreen module loaded.');
    });
})();