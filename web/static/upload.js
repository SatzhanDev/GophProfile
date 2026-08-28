(() => {
  const form = document.getElementById('upload-form');
  const dropzone = document.getElementById('dropzone');
  const dropzoneText = document.getElementById('dropzone-text');
  const fileInput = document.getElementById('file-input');
  const preview = document.getElementById('preview');
  const userIdInput = document.getElementById('user-id');
  const progressWrap = document.getElementById('progress-wrap');
  const progressBar = document.getElementById('progress-bar');
  const statusMessage = document.getElementById('status-message');

  function showPreview(file) {
    const reader = new FileReader();
    reader.onload = (e) => {
      preview.src = e.target.result;
      preview.hidden = false;
      dropzoneText.textContent = file.name;
    };
    reader.readAsDataURL(file);
  }

  dropzone.addEventListener('click', () => fileInput.click());

  dropzone.addEventListener('dragover', (e) => {
    e.preventDefault();
    dropzone.classList.add('dragover');
  });

  dropzone.addEventListener('dragleave', () => {
    dropzone.classList.remove('dragover');
  });

  dropzone.addEventListener('drop', (e) => {
    e.preventDefault();
    dropzone.classList.remove('dragover');
    if (e.dataTransfer.files.length > 0) {
      fileInput.files = e.dataTransfer.files;
      showPreview(fileInput.files[0]);
    }
  });

  fileInput.addEventListener('change', () => {
    if (fileInput.files.length > 0) {
      showPreview(fileInput.files[0]);
    }
  });

  // Прогрессивное улучшение: если этот скрипт вообще выполнился, значит
  // JS в браузере включён — перехватываем submit и грузим файл через
  // XMLHttpRequest прямо в JSON API (POST /api/v1/avatars). Так можно
  // показать прогресс-бар и не перезагружать страницу. Без JS форма всё
  // равно рабочая — уйдёт как обычный POST на /web/upload (см. атрибуты
  // action/method в upload.html).
  form.addEventListener('submit', (e) => {
    e.preventDefault();

    const userId = userIdInput.value.trim();
    if (!userId || fileInput.files.length === 0) {
      statusMessage.textContent = 'Укажите идентификатор пользователя и выберите файл';
      return;
    }

    const formData = new FormData();
    formData.append('file', fileInput.files[0]);

    const xhr = new XMLHttpRequest();
    xhr.open('POST', '/api/v1/avatars');
    xhr.setRequestHeader('X-User-ID', userId);

    statusMessage.textContent = '';
    progressWrap.hidden = false;
    progressBar.style.width = '0%';

    xhr.upload.addEventListener('progress', (event) => {
      if (event.lengthComputable) {
        const percent = Math.round((event.loaded / event.total) * 100);
        progressBar.style.width = percent + '%';
      }
    });

    xhr.onload = () => {
      if (xhr.status === 201) {
        window.location.href = '/web/gallery/' + encodeURIComponent(userId);
        return;
      }
      progressWrap.hidden = true;
      try {
        const body = JSON.parse(xhr.responseText);
        statusMessage.textContent = 'Ошибка: ' + (body.details || body.error || xhr.status);
      } catch {
        statusMessage.textContent = 'Ошибка загрузки (' + xhr.status + ')';
      }
    };

    xhr.onerror = () => {
      progressWrap.hidden = true;
      statusMessage.textContent = 'Не удалось связаться с сервером';
    };

    xhr.send(formData);
  });
})();
