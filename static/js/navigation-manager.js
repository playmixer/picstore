
/**
 * NavigationManager - клиентский менеджер навигации по изображениям
 * Использует API /api/navigation/:imageID для получения окна изображений
 * и обновляет интерфейс без перезагрузки страницы.
 */
(function() {
    'use strict';

    const DEBUG = true;
    const DEFAULT_WINDOW = 5;

    // Класс SmartPreloader
    class SmartPreloader {
        constructor(options = {}) {
            this.options = {
                windowSize: 3,
                maxPreloads: 3,
                idleDelay: 1000,
                connectionAware: true,
                cacheContexts: true,
                ...options
            };
            this.includeTags = options.includeTags || [];
            this.excludeTags = options.excludeTags || [];
            this.isPublic = options.isPublic !== undefined ? options.isPublic : true; // по умолчанию публичный
            this.preloadedContexts = new Map(); // imageID -> NavigationContext
            this.preloadedImages = new Set();   // imageURLs
            this.preloadQueue = [];
            this.isIdle = false;
            this.lastNavigationTime = 0;
            this._setupConnectionListener();
        }

        updatePreloads(currentImageID, navContext) {
            // 1. Очистить старые предзагрузки
            this._cleanupOldPreloads(currentImageID);
            // 2. Определить приоритеты
            const toPreload = this._getPreloadPriority(currentImageID, navContext);
            // 3. Выполнить предзагрузку с ограничениями
            this._executePreloads(toPreload);
            // 4. Запланировать расширенную предзагрузку при бездействии
            this._scheduleIdlePreload(currentImageID, navContext);
        }

        getPreloadedContext(imageID) {
            return this.preloadedContexts.get(imageID);
        }

        isPreloaded(imageID) {
            return this.preloadedContexts.has(imageID);
        }

        _getPreloadPriority(currentImageID, navContext) {
            const priority = [];
            // Высший приоритет: ближайшие prev/next
            if (navContext.prev && navContext.prev.length > 0) {
                priority.push({ id: navContext.prev[0].id || navContext.prev[0].ID, priority: 1 });
            }
            if (navContext.next && navContext.next.length > 0) {
                priority.push({ id: navContext.next[0].id || navContext.next[0].ID, priority: 1 });
            }
            // Средний приоритет: остальные в окне навигации
            const windowImages = [
                ...(navContext.prev || []).slice(1, this.options.windowSize),
                ...(navContext.next || []).slice(1, this.options.windowSize)
            ];
            windowImages.forEach((img, index) => {
                priority.push({ id: img.id || img.ID, priority: 2 + index });
            });
            return priority.sort((a, b) => a.priority - b.priority);
        }

        _executePreloads(priorityList) {
            // Ограничиваем количество параллельных предзагрузок
            const toLoad = priorityList.slice(0, this.options.maxPreloads);
            toLoad.forEach(item => {
                if (!this.preloadedContexts.has(item.id) && this._shouldPreload(item.id)) {
                    this._preloadNavigationContext(item.id);
                }
            });
        }

        async _preloadNavigationContext(imageID) {
            try {
                const url = new URL(`/api/navigation/${imageID}`, window.location.origin);
                url.searchParams.set('window', this.options.windowSize);
                if (this.includeTags.length) {
                    url.searchParams.set('include', this.includeTags.join(' '));
                }
                if (this.excludeTags.length) {
                    url.searchParams.set('exclude', this.excludeTags.join(' '));
                }
                // Передаём параметр публичности
                url.searchParams.set('isPublic', this.isPublic.toString());
                const response = await fetch(url);
                if (!response.ok) return;
                const context = await response.json();
                this.preloadedContexts.set(imageID, context);
                // Предзагружаем файл изображения
                if (context.current) {
                    this._preloadImageFile(context.current);
                }
            } catch (error) {
                console.warn('Failed to preload navigation context for', imageID, error);
            }
        }

        _preloadImageFile(image) {
            const base = window.location.pathname.includes('/i/view') ? '/i/view' : '/view';
            let url = `${base}/${image.path}?view=file`;
            const key = new URLSearchParams(window.location.search).get('key');
            if (key) url += `&key=${encodeURIComponent(key)}`;
            if (!this.preloadedImages.has(url)) {
                const img = new Image();
                img.src = url;
                this.preloadedImages.add(url);
            }
        }

        _cleanupOldPreloads(currentImageID) {
            const maxAge = 10;
            if (this.preloadedContexts.size > maxAge) {
                const keys = Array.from(this.preloadedContexts.keys());
                const toRemove = keys.slice(0, keys.length - maxAge);
                toRemove.forEach(key => this.preloadedContexts.delete(key));
            }
        }

        _scheduleIdlePreload(currentImageID, navContext) {
            clearTimeout(this.idleTimer);
            this.idleTimer = setTimeout(() => {
                this.isIdle = true;
                // Расширенная предзагрузка дальних изображений
                const extended = [
                    ...(navContext.prev || []).slice(this.options.windowSize, this.options.windowSize + 2),
                    ...(navContext.next || []).slice(this.options.windowSize, this.options.windowSize + 2)
                ];
                extended.forEach(img => {
                    const id = img.id || img.ID;
                    if (!this.preloadedContexts.has(id)) {
                        this._preloadNavigationContext(id);
                    }
                });
            }, this.options.idleDelay);
        }

        _shouldPreload(imageID) {
            // Уже загружено?
            if (this.preloadedContexts.has(imageID)) return false;
            // Достигнут лимит?
            if (this.preloadQueue.length >= this.options.maxPreloads) return false;
            // Пользователь активно листает?
            if (this._isUserNavigatingRapidly()) return false;
            // Слабый интернет?
            if (this.options.connectionAware && this._isConnectionPoor()) return false;
            return true;
        }

        _isUserNavigatingRapidly() {
            const now = Date.now();
            const timeSinceLastNav = now - this.lastNavigationTime;
            return timeSinceLastNav < 500; // менее 500 мс между навигациями
        }

        _isConnectionPoor() {
            if (!navigator.connection) return false;
            const conn = navigator.connection;
            return conn.saveData === true ||
                   conn.effectiveType === 'slow-2g' ||
                   conn.effectiveType === '2g';
        }

        _setupConnectionListener() {
            if (navigator.connection) {
                navigator.connection.addEventListener('change', () => {
                    // При изменении соединения можно пересмотреть стратегию
                });
            }
        }
    }

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
            this.viewsElement = document.querySelector('.view-count');
            this.imageControllerElement = document.querySelector('#imageController')
            this.encryptedPlaceholder = document.querySelector('#encrypted-placeholder');
            this.fullscreenButton = document.querySelector('.fullscreen-button');
            this.userID = this.infoElement?.dataset.userId;
            // Парсим теги из URL, переопределяя переданные опции
            this._parseTagsFromURL();
            // Создаём прелоадер с актуальными тегами и публичностью
            this.preloader = new SmartPreloader({
                includeTags: this.includeTags,
                excludeTags: this.excludeTags,
                windowSize: this.windowSize,
                isPublic: this._isPublicRoute()
            });
            this._bindEvents();
            this._loadNavigationContext();
            // Устанавливаем глобальный флаг, что NavigationManager активен
            window.navigationManagerActive = true;
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

        /**
         * Парсит строку тегов из параметра 'tags' (например, "cat -dog")
         * и заполняет массивы includeTags и excludeTags.
         */
        _parseTagsFromURL() {
            const params = new URLSearchParams(window.location.search);
            const tagsParam = params.get('tags');
            if (!tagsParam) {
                this.includeTags = [];
                this.excludeTags = [];
                return;
            }
            const tags = tagsParam.trim().split(/\s+/);
            this.includeTags = [];
            this.excludeTags = [];
            for (const tag of tags) {
                if (tag.startsWith('-')) {
                    this.excludeTags.push(tag.substring(1));
                } else {
                    this.includeTags.push(tag);
                }
            }
            this._log('Parsed tags:', { include: this.includeTags, exclude: this.excludeTags });
        }

        /**
         * Определяет, является ли текущий маршрут публичным (true) или приватным (false).
         * Публичный маршрут: /view/..., приватный: /i/view/...
         */
        _isPublicRoute() {
            return !window.location.pathname.includes('/i/view');
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
            // History API
            window.addEventListener('popstate', this._handlePopState.bind(this));
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
                // Обновляем предзагрузку
                this.preloader.updatePreloads(this.imageID, this.navContext);
            } catch (error) {
                this._log('Failed to load navigation context:', error);
                // Fallback to traditional navigation (no AJAX)
                this._disable();
            }
        }

        _buildApiUrl(imageID = null) {
            const id = imageID !== null ? imageID : this.imageID;
            const url = new URL(`${this.apiBase}/${id}`, window.location.origin);
            url.searchParams.set('window', this.windowSize);
            if (this.includeTags.length) {
                url.searchParams.set('include', this.includeTags.join(' '));
            }
            if (this.excludeTags.length) {
                url.searchParams.set('exclude', this.excludeTags.join(' '));
            }
            // Передаём параметр публичности в зависимости от маршрута
            const isPublic = this._isPublicRoute();
            url.searchParams.set('isPublic', isPublic.toString());
            return url.toString();
        }

        _updateUI() {
            if (!this.navContext) return;
            // Ближайшие предыдущее и следующее изображения (первые элементы массивов)
            const nearestPrev = this.navContext.prev && this.navContext.prev.length > 0 ? this.navContext.prev[0] : null;
            const nearestNext = this.navContext.next && this.navContext.next.length > 0 ? this.navContext.next[0] : null;
            this._log('_updateUI: navContext.prev=', this.navContext.prev, 'navContext.next=', this.navContext.next);
            this._log('_updateUI: nearestPrev=', nearestPrev, 'nearestNext=', nearestNext);
            // Обновляем кнопки навигации
            this._updateButton(this.prevButton, nearestPrev);
            this._updateButton(this.nextButton, nearestNext);
            // Обновляем скрытые изображения для предзагрузки
            this._updatePreloadImages();
            // Обновляем текущее изображение (если уже загружено)
            this.currentImage = this.navContext.current;
            this._upldateEditForm()
        }

        _upldateEditForm() {
            if (this.imageControllerElement) {
                this._log("Image controller", this.userID, this.currentImage.UserID)
                if (this.userID == this.currentImage.UserID) {
                    this.imageControllerElement.classList.remove('hide');
                } else {
                    this.imageControllerElement.classList.add('hide');
                }
                const inputTags = this.imageControllerElement.querySelector('input#tags')
                if (inputTags) {
                    inputTags.value = this.currentImage.Tags;
                }
            }
        }

        _updateButton(button, target) {
            if (!button) return;
            this._log('_updateButton:', button.id, 'target=', target, 'button.tagName=', button.tagName);
            if (target) {
                this._log('Target properties:', { id: target.id, ID: target.ID, path: target.path });
            }
            // Проверяем наличие id (может быть как id, так и ID из-за сериализации Go)
            const targetId = target ? (target.id || target.ID) : null;
            if (target && targetId) {
                this._log('Target has id', targetId);
                button.href = this._buildViewUrl(target);
                button.classList.remove('disabled');
                // Добавляем класс preloaded, если изображение предзагружено
                if (this.preloader.isPreloaded(targetId)) {
                    button.classList.add('preloaded');
                } else {
                    button.classList.remove('preloaded');
                }
            } else {
                // Нет следующего/предыдущего изображения
                this._log('No target or target.id missing', target);
                button.href = '#';
                button.classList.add('disabled');
                button.classList.remove('preloaded');
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
            this._log('Navigating to', image);
            const imageID = image.id || image.ID;
            if (!imageID) {
                console.error('Cannot navigate: image missing ID', image);
                return;
            }
            // Показываем индикатор загрузки
            this._showLoading();
            try {
                // Пытаемся использовать предзагруженный контекст
                let context = this.preloader.getPreloadedContext(imageID);
                if (!context) {
                    // Если нет предзагрузки, загружаем через API
                    context = await this._fetchNavigationContext(imageID);
                }
                if (!context) {
                    throw new Error('Failed to fetch navigation context');
                }
                // Обновляем состояние
                this.imageID = imageID;
                this.navContext = context;
                this.currentImage = context.current;
                // Обновляем DOM
                this._updateImageContent();
                // Обновляем URL в адресной строке без перезагрузки
                const newUrl = this._buildViewUrl(image);
                window.history.pushState({ imageID, context }, '', newUrl);
                // Обновляем UI (кнопки, предзагрузки)
                this._updateUI();
                this.preloader.updatePreloads(imageID, context);
                // Увеличиваем счетчик просмотров
                this._recordView(imageID);
                // Скрываем индикатор загрузки
                this._hideLoading();
            } catch (error) {
                this._log('Navigation failed:', error);
                // Fallback: переход по ссылке (полная перезагрузка)
                const fallbackUrl = this._buildViewUrl(image);
                window.location.href = fallbackUrl;
            }
        }

        async _fetchNavigationContext(imageID) {
            const url = this._buildApiUrl(imageID);
            const response = await fetch(url);
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            return await response.json();
        }

        /**
         * Отправляет запрос на увеличение счетчика просмотров для изображения.
         * @param {string|number} imageID
         */
        async _recordView(imageID) {
            try {
                const url = `/api/view/${imageID}/record`;
                const response = await fetch(url, { method: 'POST' });
                if (!response.ok) {
                    this._log('Failed to record view:', response.status);
                } else {
                    this._log('View recorded for', imageID);
                }
            } catch (error) {
                this._log('Error recording view:', error);
            }
        }

        _updateImageContent() {
            if (!this.currentImage) return;
            // Обновляем изображение
            if (this.imageElement) {
                const fileUrl = this._buildImageFileUrl(this.currentImage);
                this.imageElement.src = fileUrl;
                this.imageElement.alt = this.currentImage.Filename || 'Image';
            }
            // Обновляем информацию в блоке view-info
            if (this.infoElement) {
                // Сохраняем управляющие формы, если они есть
                const forms = this.infoElement.querySelectorAll('form');
                const formsHTML = Array.from(forms).map(f => f.outerHTML).join('');
                const hasForms = forms.length > 0;
                
                // Создаём новый HTML для блока информации
                let infoHTML = '';
                
                // Теги
                if (this.currentImage.Tags) {
                    // Tags может быть строкой или массивом
                    let tagsArray = [];
                    if (Array.isArray(this.currentImage.Tags)) {
                        tagsArray = this.currentImage.Tags;
                    } else if (typeof this.currentImage.Tags === 'string' && this.currentImage.Tags.trim() !== '') {
                        tagsArray = this.currentImage.Tags.trim().split(/\s+/);
                    }
                    if (tagsArray.length > 0) {
                        infoHTML += '<p><strong>Теги:</strong></p>';
                        infoHTML += '<div class="tags">';
                        tagsArray.forEach(tag => {
                            infoHTML += `<span class="tag">${this._escapeHtml(tag)}</span>`;
                        });
                        infoHTML += '</div>';
                    }
                }
                
                // Просмотры
                const views = this.currentImage.Views || 0;
                infoHTML += `<p><strong>Просмотры:</strong> <span class="view-count" data-update="views">${views}</span></p>`;
                
                // Если есть формы управления, добавляем их обратно
                if (hasForms) {
                    infoHTML += '<div id="imageController" class="hide">';
                    infoHTML += '<hr style="margin: 20px 0;">';
                    infoHTML += '<h3>Управление изображением</h3>';
                    infoHTML += formsHTML;
                    infoHTML += '</div>';
                }
                
                // Заменяем содержимое, но сохраняем классы и структуру
                this.infoElement.innerHTML = infoHTML;
                
                // После замены нужно заново получить ссылки на элементы
                this.tagsElement = this.infoElement.querySelector('.tags');
                this.viewsElement = this.infoElement.querySelector('.view-count');
                // Обновляем зашифрованный плейсхолдер (если есть)
                if (this.encryptedPlaceholder) {
                    if (this.currentImage.encrypted) {
                        this.encryptedPlaceholder.classList.remove('hide');
                    } else {
                        this.encryptedPlaceholder.classList.add('hide');
                    }
                }
                this.imageControllerElement = document.querySelector('#imageController')
                this._upldateEditForm()
                // Обновляем action форм, чтобы они вели к текущему изображению
                this._updateFormsAction();
            } else {
                // Если infoElement не найден, пытаемся обновить отдельные элементы
                if (this.tagsElement) {
                    this.tagsElement.innerHTML = '';
                    if (this.currentImage.Tags) {
                        let tagsArray = [];
                        if (Array.isArray(this.currentImage.Tags)) {
                            tagsArray = this.currentImage.Tags;
                        } else if (typeof this.currentImage.Tags === 'string' && this.currentImage.Tags.trim() !== '') {
                            tagsArray = this.currentImage.Tags.trim().split(/\s+/);
                        }
                        tagsArray.forEach(tag => {
                            const span = document.createElement('span');
                            span.className = 'tag';
                            span.textContent = tag;
                            this.tagsElement.appendChild(span);
                        });
                    }
                }
                if (this.viewsElement) {
                    this.viewsElement.textContent = this.currentImage.Views || 0;
                }
                if (this.encryptedPlaceholder) {
                    if (this.currentImage.encrypted) {
                        this.encryptedPlaceholder.classList.remove('hide');
                    } else {
                        this.encryptedPlaceholder.classList.add('hide');
                    }
                }
                this._upldateEditForm()
            }
            // Обновляем заголовок страницы
            if (this.currentImage.Filename) {
                document.title = this.currentImage.Filename + ' - PicStore';
            } else if (this.currentImage.path) {
                // извлекаем имя файла из пути
                const filename = this.currentImage.path.split('/').pop();
                document.title = filename + ' - PicStore';
            }
            // Обновляем скрытый div #debug-nav (если есть)
            const debugNav = document.getElementById('debug-nav');
            if (debugNav) {
                const nearestPrev = this.navContext && this.navContext.prev && this.navContext.prev.length > 0 ? this.navContext.prev[0] : null;
                const nearestNext = this.navContext && this.navContext.next && this.navContext.next.length > 0 ? this.navContext.next[0] : null;
                debugNav.dataset.prev = nearestPrev ? (nearestPrev.id || nearestPrev.ID) : '';
                debugNav.dataset.next = nearestNext ? (nearestNext.id || nearestNext.ID) : '';
            }
        }
        
        _escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }
        
        _updateFormsAction() {
            // Обновляем action форм управления, чтобы они вели к текущему изображению
            const forms = this.infoElement.querySelectorAll('form');
            const formEncrypt = window.document.querySelector('form#formEncrypt')
            const base = window.location.pathname.includes('/i/view') ? '/i/view' : '/view';
            const path = this.currentImage.path;
            const params = new URLSearchParams(window.location.search);
            const tagsParam = params.get('tags') || '';
            const keyParam = params.get('key') || '';
            
            forms.forEach(form => {
                const action = form.getAttribute('action');
                const method = form.getAttribute('method');
                if (action && action.includes('/update')) {
                    // Форма обновления
                    const newAction = `/i/view/${path}/update?tags=${encodeURIComponent(tagsParam)}&key=${encodeURIComponent(keyParam)}`;
                    form.setAttribute('action', newAction);
                } else if (action && action.includes('/delete')) {
                    // Форма удаления
                    const newAction = `/i/view/${path}/delete?tags=${encodeURIComponent(tagsParam)}&key=${encodeURIComponent(keyParam)}`;
                    form.setAttribute('action', newAction);
                }
            });

            if (formEncrypt) {
                const newAction = `${base}/${path}?tags=${encodeURIComponent(tagsParam)}&key=${encodeURIComponent(keyParam)}`;
                formEncrypt.setAttribute('action', newAction);
            }
        }

        _handlePopState(event) {
            if (event.state && event.state.imageID) {
                this.imageID = event.state.imageID;
                this.navContext = event.state.context;
                this.currentImage = this.navContext.current;
                this._updateImageContent();
                this._updateUI();
                this.preloader.updatePreloads(this.imageID, this.navContext);
            }
        }

        _showLoading() {
            // Создаём индикатор загрузки, если его нет
            let loader = document.getElementById('navigation-loader');
            if (!loader) {
                loader = document.createElement('div');
                loader.id = 'navigation-loader';
                loader.className = 'navigation-loader';
                loader.innerHTML = 'Loading...';
                document.body.appendChild(loader);
            }
            loader.style.display = 'block';
        }

        _hideLoading() {
            const loader = document.getElementById('navigation-loader');
            if (loader) {
                loader.style.display = 'none';
            }
        }

        _disable() {
            // Отключаем менеджер, оставляем обычную навигацию
            window.navigationManagerActive = false;
            this._log('Navigation manager disabled');
        }

        _log(...args) {
            if (DEBUG) {
                console.log('[NavigationManager]', ...args);
            }
        }
    }

    // Автоматическая инициализация при наличии необходимых элементов
    if (document.querySelector('.view-image-container') && (document.getElementById('prev-button') || document.getElementById('next-button'))) {
        new NavigationManager();
    }
})();