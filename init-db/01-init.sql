-- Database Initialization Schema
CREATE TABLE IF NOT EXISTS photos (
    id SERIAL PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    image_url VARCHAR(500) NOT NULL,
    likes_count INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS comments (
    id SERIAL PRIMARY KEY,
    photo_id INT REFERENCES photos (id) ON DELETE CASCADE,
    author VARCHAR(100) NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Seed 15 photos
INSERT INTO photos (title, image_url, likes_count)
SELECT 
    'Cloud Native Infrastructure #' || i,
    '/static/images/photo-' || i || '.jpg',
    (RANDOM() * 300 + 45)::INT
FROM generate_series(1, 15) AS i;