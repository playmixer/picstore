/**
 * Keyboard and touch navigation for image viewing (left/right arrows and swipe)
 */
(function() {
    'use strict';

    // Wait for DOM to be fully loaded
    document.addEventListener('DOMContentLoaded', function() {
        // Find navigation buttons by ID
        const prevButton = document.getElementById('prev-button');
        const nextButton = document.getElementById('next-button');

        // Helper to check if element is disabled (span or has disabled class)
        function isDisabled(el) {
            if (!el) return true;
            if (el.tagName === 'SPAN') return true;
            if (el.classList.contains('disabled')) return true;
            return false;
        }

        // Navigate to URL if link is active
        function navigateTo(url) {
            if (url) {
                window.location.href = url;
            }
        }

        // Keydown handler
        function handleKeydown(event) {
            // Ignore if target is an input, textarea, select, or contenteditable
            const tag = event.target.tagName;
            if (['INPUT', 'TEXTAREA', 'SELECT'].includes(tag) ||
                event.target.isContentEditable) {
                return;
            }

            switch (event.key) {
                case 'ArrowLeft':
                case 'Left': // IE/Edge
                    if (prevButton && !isDisabled(prevButton)) {
                        event.preventDefault();
                        navigateTo(prevButton.href);
                    }
                    break;
                case 'ArrowRight':
                case 'Right': // IE/Edge
                    if (nextButton && !isDisabled(nextButton)) {
                        event.preventDefault();
                        navigateTo(nextButton.href);
                    }
                    break;
            }
        }

        // Touch swipe handling
        let touchStartX = 0;
        let touchEndX = 0;
        const swipeThreshold = 50; // minimum distance in pixels to trigger swipe

        function handleTouchStart(event) {
            touchStartX = event.changedTouches[0].screenX;
        }

        function handleTouchMove(event) {
            // Prevent vertical scrolling if we detect horizontal movement?
            // We'll allow default behavior for now.
        }

        function handleTouchEnd(event) {
            touchEndX = event.changedTouches[0].screenX;
            const diffX = touchStartX - touchEndX;

            // Determine swipe direction
            if (Math.abs(diffX) > swipeThreshold) {
                if (diffX > 0) {
                    // Swipe left -> go to next image
                    if (nextButton && !isDisabled(nextButton)) {
                        navigateTo(nextButton.href);
                    }
                } else {
                    // Swipe right -> go to previous image
                    if (prevButton && !isDisabled(prevButton)) {
                        navigateTo(prevButton.href);
                    }
                }
            }
        }

        // Attach event listeners
        document.addEventListener('keydown', handleKeydown);
        document.addEventListener('touchstart', handleTouchStart, { passive: true });
        document.addEventListener('touchmove', handleTouchMove, { passive: true });
        document.addEventListener('touchend', handleTouchEnd, { passive: true });

        // Optional: log for debugging
        console.log('Keyboard and touch navigation loaded. Use ←/→ or swipe to navigate images.');
        console.log('Prev button:', prevButton ? (isDisabled(prevButton) ? 'disabled' : 'active') : 'not found');
        console.log('Next button:', nextButton ? (isDisabled(nextButton) ? 'disabled' : 'active') : 'not found');
    });
})();