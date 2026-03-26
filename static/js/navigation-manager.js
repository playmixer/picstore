/**
 * NavigationManager - клиентский менеджер навигации по изображениям
 * Использует API /api/navigation/:imageID для получения окна изображений
 * и обновляет интерфейс без перезагрузки страницы.
 */
(function() {
    'use strict';

    const DEBUG = true;
    const DEFAULT_WINDOW = 5;

    class NavigationManager {
        constructor(options = {}) {
            this.imageID = options.imageID || this._extractImageID();
            this.windowSize = options.windowSize || DEFAULT_WINDOW;
            this.includeTags = options.includeTags || [];
            this.excludeTags = options.excludeTags || [];
            this.apiBase = options.apiBase || '/api/navigation';
            this.currentImage = null;
            this.navContext = null;
            this.prevButton = document.getElementById('prev-button');
            this.nextButton = document.getElementById('next-button');
            this.imageElement = document.querySelector('.view-image');
            this.container = document.querySelector('.view-image-container');
            this.infoElement = document.querySelector('.view-info');
            this.tagsElement = document.querySelector('.tags');
            // this.viewsElement = document.querySelector('p strong:contains("Просмотры:")')?.parentElement; // невалидный селектор, временно отключено
            this.encryptedPlaceholder = document.querySelector('.encrypted-placeholder');
            this.fullscreenButton = document.querySelector('.fullscreen-button');
            this._bindEvents();
            this._loadNavigationContext();
        }

        _extractImageID() {
            // Попробуем получить из data-атрибута
            const dataEl = document.querySelector('[data-image-id]');
            if (dataEl) {
                return parseInt(dataEl.dataset.imageId, 10);
            }
            // Иначе из URL (например, /view/...)
            const match = window.location.pathname.match(/\/view\/\d+\/\d+\/\d+\/\d+\/(.+)/);
            if (match) {
                // Не можем получить ID из пути, нужно другое решение
                // Вернём null, тогда менеджер не будет работать
                return null;
            }
            return null;
        }

        _bindEvents() {
            if (this.prevButton && this.prevButton.tagName === 'A') {
                this.prevButton.addEventListener('click', (e) => {
                    e.preventDefault();
                    this.navigateToPrev();
                });
            }
            if (this.nextButton && this.nextButton.tagName === 'A') {
                this.nextButton.addEventListener('click', (e) => {
                    e.preventDefault();
                    this.navigateToNext();
                });
            }
            // Перехватываем клавиатурные события, чтобы не конфликтовать с view-keys.js
            document.addEventListener('keydown', (e) => {
                if (['INPUT', 'TEXTAREA', 'SELECT'].includes(e.target.tagName) || e.target.isContentEditable) {
                    return;
                }
                if (e.key === 'ArrowLeft' || e.key === 'Left') {
                    e.preventDefault();
                    this.navigateToPrev();
                } else if (e.key === 'ArrowRight' || e.key === 'Right') {
                    e.preventDefault();
                    this.navigateToNext();
                }
            });
            // Свайпы
            this._setupSwipe();
        }

        _setupSwipe() {
            let touchStartX = 0;
            const threshold = 50;
            const handleTouchStart = (e) => {
                touchStartX = e.changedTouches[0].screenX;
            };
            const handleTouchEnd = (e) => {
                const touchEndX = e.changedTouches[0].screenX;
                const diffX = touchStartX - touchEndX;
                if (Math.abs(diffX) > threshold) {
                    if (diffX > 0) {
                        this.navigateToNext();
                    } else {
                        this.navigateToPrev();
                    }
                }
            };
            if (this.container) {
                this.container.addEventListener('touchstart', handleTouchStart, { passive: true });
                this.container.addEventListener('touchend', handleTouchEnd, { passive: true });
            }
        }

        async _loadNavigationContext() {
            if (!this.imageID) {
                this._log('ImageID not found, navigation manager disabled.');
                return;
            }
            try {
                const url = this._buildApiUrl();
                this._log('Fetching navigation context from', url);
                const response = await fetch(url);
                if (!response.ok) {
                    throw new Error(`HTTP ${response.status}: ${response.statusText}`);
                }
                this.navContext = await response.json();
                this._log('Navigation context loaded:', this.navContext);
                this._updateUI();
            } catch (error) {
                this._log('Failed to load navigation context:', error);
                // Fallback to traditional navigation (no AJAX)
                this._disable();
            }
        }

        _buildApiUrl() {
            const url = new URL(`${this.apiBase}/${this.imageID}`, window.location.origin);
            url.searchParams.set('window', this.windowSize);
            if (this.includeTags.length) {
                url.searchParams.set('include', this.includeTags.join(' '));
            }
            if (this.excludeTags.length) {
                url.searchParams.set('exclude', this.excludeTags.join(' '));
            }
            return url.toString();
        }

        _updateUI() {
            if (!this.navContext) return;
            // Ближайшие предыдущее и следующее изображения (первые элементы массивов)
            const nearestPrev = this.navContext.prev && this.navContext.prev.length > 0 ? this.navContext.prev[0] : null;
            const nearestNext = this.navContext.next && this.navContext.next.length > 0 ? this.navContext.next[0] : null;
            this._log('_updateUI: nearestPrev=', nearestPrev, 'nearestNext=', nearestNext);
            // Обновляем кнопки навигации
            this._updateButton(this.prevButton, nearestPrev);
            this._updateButton(this.nextButton, nearestNext);
            // Обновляем скрытые изображения для предзагрузки
            this._updatePreloadImages();
            // Обновляем текущее изображение (если уже загружено)
            this.currentImage = this.navContext.current;
        }

        _updateButton(button, target) {
            if (!button) return;
            this._log('_updateButton:', button.id, 'target=', target, 'button.tagName=', button.tagName);
            // Проверяем наличие id (может быть как id, так и ID из-за сериализации Go)
            const targetId = target ? (target.id || target.ID) : null;
            if (target && targetId) {
                this._log('Target has id', targetId);
                button.href = this._buildViewUrl(target);
                button.classList.remove('disabled');
                if (button.tagName === 'SPAN') {
                    // Заменяем span на a
                    const a = document.createElement('a');
                    a.id = button.id;
                    a.className = button.className.replace('disabled', '').trim();
                    a.href = button.href;
                    a.innerHTML = button.innerHTML;
                    button.parentNode.replaceChild(a, button);
                    // Перепривязываем событие
                    a.addEventListener('click', (e) => {
                        e.preventDefault();
                        if (button.id === 'prev-button') this.navigateToPrev();
                        else this.navigateToNext();
                    });
                    this._log('Replaced span with a, href=', a.href);
                } else {
                    this._log('Button is already an A, href updated to', button.href);
                }
            } else {
                // Нет следующего/предыдущего изображения
                this._log('No target or target.id missing', target);
                button.href = '#';
                button.classList.add('disabled');
                if (button.tagName === 'A') {
                    // Заменяем a на span
                    const span = document.createElement('span');
                    span.id = button.id;
                    span.className = button.className + ' disabled';
                    span.innerHTML = button.innerHTML;
                    button.parentNode.replaceChild(span, button);
                    this._log('Replaced a with span');
                } else {
                    this._log('Button is already a span');
                }
            }
        }

        _buildViewUrl(image) {
            // image { id, path, ... }
            const base = window.location.pathname.includes('/i/view') ? '/i/view' : '/view';
            let url = `${base}/${image.path}`;
            const params = new URLSearchParams(window.location.search);
            // Сохраняем теги и ключ
            if (params.has('tags')) url += `?tags=${encodeURIComponent(params.get('tags'))}`;
            if (params.has('key')) url += (url.includes('?') ? '&' : '?') + `key=${encodeURIComponent(params.get('key'))}`;
            return url;
        }

        _updatePreloadImages() {
            // Удаляем старые скрытые изображения
            document.querySelectorAll('img[data-preload]').forEach(img => img.remove());
            // Предзагружаем все изображения из окна навигации
            const preloadImages = [];
            if (this.navContext.prev && Array.isArray(this.navContext.prev)) {
                this.navContext.prev.forEach(imgObj => preloadImages.push(imgObj));
            }
            if (this.navContext.next && Array.isArray(this.navContext.next)) {
                this.navContext.next.forEach(imgObj => preloadImages.push(imgObj));
            }
            // Ограничим количество предзагружаемых изображений, чтобы не перегружать сеть
            const MAX_PRELOAD = 10;
            preloadImages.slice(0, MAX_PRELOAD).forEach((imgObj, index) => {
                const img = document.createElement('img');
                img.src = this._buildImageFileUrl(imgObj);
                img.style.display = 'none';
                img.setAttribute('data-preload', `window-${index}`);
                document.body.appendChild(img);
                this._log('Preloading image', img.src);
            });
            // Также можно использовать link[rel=preload] для более агрессивной предзагрузки
            // но это сложнее управлять, оставим на будущее
        }

        _buildImageFileUrl(image) {
            const base = window.location.pathname.includes('/i/view') ? '/i/view' : '/view';
            let url = `${base}/${image.path}?view=file`;
            const key = new URLSearchParams(window.location.search).get('key');
            if (key) url += `&key=${encodeURIComponent(key)}`;
            return url;
        }

        async navigateToPrev() {
            if (!this.navContext || !this.navContext.prev || this.navContext.prev.length === 0) return;
            const nearestPrev = this.navContext.prev[0];
            await this._navigateToImage(nearestPrev);
        }

        async navigateToNext() {
            if (!this.navContext || !this.navContext.next || this.navContext.next.length === 0) return;
            const nearestNext = this.navContext.next[0];
            await this._navigateToImage(nearestNext);
        }

        async _navigateToImage(image) {
            this._log('Navigating to image', image);
            // Показываем индикатор загрузки
            this._showLoading();
            // Загружаем новую страницу через AJAX? Или просто обновляем данные?
            // Для простоты сделаем переход по ссылке, но с предзагрузкой контекста.
            // Можно загрузить новый NavigationContext для нового imageID и обновить страницу без перезагрузки.
            // Пока что сделаем простой переход, но с сохранением состояния.
            window.location.href = this._buildViewUrl(image);
        }

        _showLoading() {
            // Добавляем overlay с индикатором
            const overlay = document.createElement('div');
            overlay.id = 'nav-loading-overlay';
            overlay.style.position = 'fixed';
            overlay.style.top = '0';
            overlay.style.left = '0';
            overlay.style.width = '100%';
            overlay.style.height = '100%';
            overlay.style.backgroundColor = 'rgba(255,255,255,0.7)';
            overlay.style.zIndex = '10000';
            overlay.style.display = 'flex';
            overlay.style.alignItems = 'center';
            overlay.style.justifyContent = 'center';
            overlay.innerHTML = '<div style="font-size: 24px;">Загрузка...</div>';
            document.body.appendChild(overlay);
            setTimeout(() => {
                if (document.body.contains(overlay)) {
                    document.body.removeChild(overlay);
                }
            }, 3000);
        }

        _disable() {
            this._log('NavigationManager disabled, falling back to traditional navigation.');
            // Отключаем наши обработчики, чтобы не мешать view-keys.js
            // (они останутся работать)
        }

        _log(...args) {
            if (DEBUG) {
                console.log('[NavigationManager]', ...args);
            }
        }
    }

    // Автоматическая инициализация при загрузке DOM
    document.addEventListener('DOMContentLoaded', () => {
        // Проверяем, находимся ли на странице просмотра
        if (document.querySelector('.view-container')) {
            window.navigationManager = new NavigationManager();
        }
    });

})();