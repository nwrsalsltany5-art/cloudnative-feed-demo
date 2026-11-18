    const demoAuthors = [
      { name: "Michael Bruno", bio: "Traveler, Life Lover", avatar: "https://images.unsplash.com/photo-1534528741775-53994a69daeb?w=100&auto=format&fit=crop&q=80" },
      { name: "Emma Stone", bio: "Photographer & Visual Artist", avatar: "https://images.unsplash.com/photo-1517841905240-472988babdf9?w=100&auto=format&fit=crop&q=80" },
      { name: "Lucas Vance", bio: "DevOps Engineer @ CloudNative", avatar: "https://images.unsplash.com/photo-1507003211169-0a1dd7228f2d?w=100&auto=format&fit=crop&q=80" },
      { name: "Sophia Chen", bio: "Kubernetes Architect", avatar: "https://images.unsplash.com/photo-1494790108377-be9c29b29330?w=100&auto=format&fit=crop&q=80" },
      { name: "David Miller", bio: "Site Reliability Specialist", avatar: "https://images.unsplash.com/photo-1500648767791-00dcc994a43e?w=100&auto=format&fit=crop&q=80" }
    ];

    const commentAvatars = {
      "Alex Rivera": "https://images.unsplash.com/photo-1535713875002-d1d0cf377fde?w=80&q=80",
      "DevOps_Bot": "https://images.unsplash.com/photo-1570295999919-56ceb5ecca61?w=80&q=80",
      "Sara Connor": "https://images.unsplash.com/photo-1494790108377-be9c29b29330?w=80&q=80",
      "Marcus Vance": "https://images.unsplash.com/photo-1507003211169-0a1dd7228f2d?w=80&q=80",
      "Elena Rostova": "https://images.unsplash.com/photo-1517841905240-472988babdf9?w=80&q=80",
      "K8s_Admin": "https://images.unsplash.com/photo-1500648767791-00dcc994a43e?w=80&q=80",
      "Audience User": "https://images.unsplash.com/photo-1534528741775-53994a69daeb?w=100&auto=format&fit=crop&q=80"
    };

    const likedPhotosMap = new Set();
    const photoCommentsMap = {};
    let activePhotoIdForComments = null;

    async function fetchFeed() {
      try {
        const response = await fetch('/api/feed');
        if (!response.ok) throw new Error(`Feed request failed with status ${response.status}`);
        const photos = await response.json();
        likedPhotosMap.clear();
        photos.forEach(photo => {
          if (photo.liked) likedPhotosMap.add(photo.id);
        });
        renderFeed(photos);
      } catch (err) {
        console.error("Failed to load feed:", err);
        showToast("Unable to load the feed. Please try again.");
      }
    }

    function renderFeed(photos) {
      const container = document.getElementById('feed-container');
      let html = '';

      photos.forEach((photo, idx) => {
        const author = demoAuthors[idx % demoAuthors.length];
        const shareCount = 12 + (photo.id * 3);

        const commentsCount = photo.comments_count || 0;
        const isLiked = likedPhotosMap.has(photo.id);

        html += `
                    <article class="post-card" id="post-${photo.id}">
                        <div class="post-header">
                            <div class="user-info">
                                <img class="avatar-img" src="${author.avatar}" alt="${author.name}">
                                <div class="user-details">
                                    <span class="user-name">${author.name}</span>
                                    <span class="user-bio">${author.bio}</span>
                                </div>
                            </div>
                            <button class="options-btn" data-action="toast" data-toast="⚙️ Post options coming soon">
                                <svg width="20" height="20" viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="5" r="2"/><circle cx="12" cy="12" r="2"/><circle cx="12" cy="19" r="2"/></svg>
                            </button>
                        </div>
                        <div class="image-wrapper" data-action="image" data-photo-id="${photo.id}">
                            <img class="post-image" src="${escapeHtml(photo.image_url)}" alt="${escapeHtml(photo.title)}">
                            <div class="bookmark-badge" data-action="bookmark">
                                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path></svg>
                            </div>
                            <div class="center-heart-pop" id="heart-pop-${photo.id}">
                                <svg width="80" height="80" viewBox="0 0 24 24" fill="currentColor"><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg>
                            </div>
                        </div>
                        <div class="action-bar">
                            <button class="action-item ${isLiked ? 'liked' : ''}" id="like-btn-${photo.id}" data-action="like-photo" data-photo-id="${photo.id}">
                                <svg viewBox="0 0 24 24"><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg>
                                <span id="like-count-${photo.id}">${photo.likes_count}</span>
                            </button>
                            <button class="action-item" data-action="open-comments" data-photo-id="${photo.id}">
                                <svg viewBox="0 0 24 24"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path></svg>
                                <span id="comment-count-${photo.id}">${commentsCount}</span>
                            </button>
                            <button class="action-item" data-action="toast" data-toast="🚀 Share Feature Coming Soon">
                                <svg viewBox="0 0 24 24"><circle cx="18" cy="5" r="3"></circle><circle cx="6" cy="12" r="3"></circle><circle cx="18" cy="19" r="3"></circle><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"></line><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"></line></svg>
                                <span>${shareCount}</span>
                            </button>
                        </div>
                    </article>
                `;
      });
      container.innerHTML = html;
      container.querySelectorAll(".post-image").forEach(image => {
        image.addEventListener("error", () => {
          image.src = "/static/images/photo-1.jpg";
        }, { once: true });
      });
    }

    let lastTapTime = 0;
    function handleImageTap(photoId, event) {
      const currentTime = new Date().getTime();
      const tapLength = currentTime - lastTapTime;
      if (tapLength < 300 && tapLength > 0) {
        triggerHeartPop(photoId);
        if (!likedPhotosMap.has(photoId)) toggleLikePhoto(photoId);
        event.preventDefault();
      }
      lastTapTime = currentTime;
    }

    function triggerHeartPop(photoId) {
      const heart = document.getElementById(`heart-pop-${photoId}`);
      if (!heart) return;
      heart.classList.remove('animate');
      void heart.offsetWidth;
      heart.classList.add('animate');
    }

    async function toggleLikePhoto(photoId) {
      const likeBtn = document.getElementById(`like-btn-${photoId}`);
      const countSpan = document.getElementById(`like-count-${photoId}`);
      if (likedPhotosMap.has(photoId) || likeBtn.disabled) return;
      likeBtn.disabled = true;
      triggerHeartPop(photoId);
      try {
        const response = await fetch(`/api/photos/${photoId}/like`, { method: 'POST' });
        if (!response.ok) throw new Error(`Like request failed with status ${response.status}`);
        const data = await response.json();
        likedPhotosMap.add(photoId);
        likeBtn.classList.add('liked');
        countSpan.innerText = data.likes;
      } catch (error) {
        console.error("Like API error:", error);
        showToast("Unable to like this photo. Please try again.");
      } finally {
        likeBtn.disabled = false;
      }
    }

    function openCommentsModal(photoId) {
      activePhotoIdForComments = photoId;
      document.getElementById('comments-overlay').classList.add('active');
      loadComments(photoId);
    }

    function closeCommentsModal() {
      document.getElementById('comments-overlay').classList.remove('active');
      activePhotoIdForComments = null;
    }

    function renderCommentsList(photoId) {
      const commentsList = document.getElementById('modal-comments-list');
      const comments = photoCommentsMap[photoId] || [];
      const podName = escapeHtml(document.getElementById('pod-name-display').innerText);

      commentsList.innerHTML = comments.map(c => `
                <div class="comment-item">
                    <img class="comment-avatar" src="${c.avatar}" alt="${escapeHtml(c.author)}">
                    <div class="comment-content">
                        <div class="comment-header-row">
                            <span class="comment-author">${escapeHtml(c.author)}</span>
                            <span class="comment-time">${escapeHtml(c.time)}</span>
                        </div>
                        <div class="comment-text">
                            ${escapeHtml(c.text)} ${c.isUser ? `<span class="comment-pod-tag">${podName}</span>` : ''}
                        </div>
                    </div>
                    <button class="comment-like-btn ${c.liked ? 'liked' : ''}" id="comment-like-${Number(c.id)}" data-action="like-comment" data-photo-id="${Number(photoId)}" data-comment-id="${Number(c.id)}">
                        <svg viewBox="0 0 24 24"><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg>
                        <span>${c.likes > 0 ? c.likes : ''}</span>
                    </button>
                </div>
            `).join('');
    }

    async function toggleCommentLike(photoId, commentId) {
      const comments = photoCommentsMap[photoId];
      if (!comments) return;
      const comment = comments.find(c => c.id === Number(commentId));
      const likeButton = document.getElementById(`comment-like-${commentId}`);
      if (!comment || comment.liked || likeButton.disabled) return;
      likeButton.disabled = true;
      try {
        const response = await fetch(`/api/comments/${commentId}/like`, { method: 'POST' });
        if (!response.ok) throw new Error(`Comment like request failed with status ${response.status}`);
        const result = await response.json();
        comment.liked = true;
        comment.likes = result.likes;
        renderCommentsList(photoId);
      } catch (error) {
        console.error("Comment like API error:", error);
        showToast("Unable to like this comment. Please try again.");
      } finally {
        const updatedButton = document.getElementById(`comment-like-${commentId}`);
        if (updatedButton) updatedButton.disabled = false;
      }
    }

    async function loadComments(photoId) {
      try {
        const response = await fetch(`/api/photos/${photoId}/comments`);
        if (!response.ok) throw new Error(`Comments request failed with status ${response.status}`);
        const savedComments = await response.json();
        photoCommentsMap[photoId] = savedComments.map(comment => ({
          id: comment.id,
          author: comment.author,
          avatar: commentAvatars[comment.author] || commentAvatars["Audience User"],
          text: comment.message,
          likes: comment.likes_count,
          liked: comment.liked,
          time: formatCommentTime(comment.created_at),
          isUser: comment.author === "Audience User"
        }));
        if (activePhotoIdForComments === photoId) renderCommentsList(photoId);

        const countSpan = document.getElementById(`comment-count-${photoId}`);
        if (countSpan) countSpan.innerText = savedComments.length;
      } catch (err) {
        console.error("Failed to load comments:", err);
        showToast("Unable to load comments. Please try again.");
      }
    }

    function formatCommentTime(createdAt) {
      const elapsedMinutes = Math.max(0, Math.floor((Date.now() - new Date(createdAt).getTime()) / 60000));
      if (elapsedMinutes < 1) return "Just now";
      if (elapsedMinutes < 60) return `${elapsedMinutes}m`;
      const elapsedHours = Math.floor(elapsedMinutes / 60);
      if (elapsedHours < 24) return `${elapsedHours}h`;
      return `${Math.floor(elapsedHours / 24)}d`;
    }

    function escapeHtml(value) {
      return String(value).replace(/[&<>"']/g, character => ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&#39;"
      })[character]);
    }

    function handleCommentKeyPress(e) {
      if (e.key === 'Enter') submitModalComment();
    }

    async function submitModalComment() {
      const input = document.getElementById('sheet-comment-input');
      const message = input.value.trim();
      if (!message || !activePhotoIdForComments) return;

      const photoId = activePhotoIdForComments;
      try {
        const response = await fetch(`/api/photos/${photoId}/comment`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ message: message })
        });
        if (!response.ok) throw new Error(`Comment request failed with status ${response.status}`);
        const comment = await response.json();
        photoCommentsMap[photoId] = photoCommentsMap[photoId] || [];
        photoCommentsMap[photoId].push({
          id: comment.id,
          author: comment.author,
          avatar: commentAvatars[comment.author] || commentAvatars["Audience User"],
          text: comment.message,
          likes: comment.likes_count,
          liked: false,
          time: "Just now",
          isUser: comment.author === "Audience User"
        });
        if (activePhotoIdForComments === photoId) {
          renderCommentsList(photoId);
          const commentsList = document.getElementById('modal-comments-list');
          commentsList.scrollTop = commentsList.scrollHeight;
        }
        input.value = '';

        const countSpan = document.getElementById(`comment-count-${photoId}`);
        if (countSpan) countSpan.innerText = photoCommentsMap[photoId].length;
      } catch (err) {
        console.error("Comment API error:", err);
        showToast("Unable to post your comment. Please try again.");
      }
    }

    let toastTimeout;
    function showToast(message) {
      const toast = document.getElementById('toast-banner');
      toast.innerText = message;
      toast.classList.add('show');
      clearTimeout(toastTimeout);
      toastTimeout = setTimeout(() => toast.classList.remove('show'), 2200);
    }

    document.addEventListener("click", event => {
      const action = event.target.closest("[data-action]");
      if (!action) return;
      switch (action.dataset.action) {
        case "toast":
          showToast(action.dataset.toast);
          break;
        case "bookmark":
          showToast("🔖 Bookmark Saved!");
          break;
        case "image":
          handleImageTap(Number(action.dataset.photoId), event);
          break;
        case "like-photo":
          toggleLikePhoto(Number(action.dataset.photoId));
          break;
        case "open-comments":
          openCommentsModal(Number(action.dataset.photoId));
          break;
        case "close-comments":
          closeCommentsModal();
          break;
        case "submit-comment":
          submitModalComment();
          break;
        case "like-comment":
          toggleCommentLike(Number(action.dataset.photoId), Number(action.dataset.commentId));
          break;
      }
    });
    document.getElementById("sheet-comment-input").addEventListener("keydown", handleCommentKeyPress);

    fetchFeed();
