(() => {
  document.querySelectorAll('.delete-btn').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const avatarId = btn.dataset.avatarId;
      const userId = btn.dataset.userId;

      if (!confirm('Удалить эту аватарку?')) {
        return;
      }

      try {
        // DELETE с кастомным заголовком X-User-ID — то, что обычная HTML-форма
        // отправить не умеет (у форм нет метода DELETE и нет заголовков),
        // поэтому удаление в галерее возможно только через этот JS.
        const res = await fetch('/api/v1/avatars/' + encodeURIComponent(avatarId), {
          method: 'DELETE',
          headers: { 'X-User-ID': userId },
        });

        if (res.status === 204) {
          btn.closest('.card').remove();
          return;
        }

        const body = await res.json().catch(() => ({}));
        alert('Не удалось удалить: ' + (body.details || body.error || res.status));
      } catch (err) {
        alert('Не удалось связаться с сервером');
      }
    });
  });
})();
