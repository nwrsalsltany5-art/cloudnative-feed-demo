update public.photos
set likes_count = greatest(likes_count, 20 + id)
where id between 1 and 15;

create or replace function public.get_comments(p_photo_id integer, p_visitor_key text)
returns table (
    id integer,
    author text,
    message text,
    likes_count integer,
    created_at timestamptz,
    liked boolean
)
language plpgsql
security definer
set search_path = pg_catalog, public, pg_temp
as $$
begin
    if not exists (
        select 1 from public.photos p where p.id = p_photo_id
    ) then
        raise exception 'photo not found' using errcode = 'P0002';
    end if;

    return query
        select c.id, c.author, c.message, c.likes_count, c.created_at,
               exists (
                   select 1
                   from public.comment_likes cl
                   where cl.comment_id = c.id
                     and cl.visitor_key = p_visitor_key
               )
        from public.comments c
        where c.photo_id = p_photo_id
        order by c.created_at, c.id;
end;
$$;

create or replace function public.like_photo(p_photo_id integer, p_visitor_key text)
returns jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, pg_temp
as $$
declare
    current_count integer;
    deleted_count integer;
    inserted_count integer;
    current_liked boolean;
begin
    if char_length(p_visitor_key) <> 64 then
        raise exception 'invalid visitor key' using errcode = '22023';
    end if;

    perform 1
    from public.photos p
    where p.id = p_photo_id
    for update;
    if not found then
        raise exception 'photo not found' using errcode = 'P0002';
    end if;

    delete from public.photo_likes
    where photo_id = p_photo_id and visitor_key = p_visitor_key;
    get diagnostics deleted_count = row_count;

    if deleted_count > 0 then
        update public.photos
        set likes_count = greatest(likes_count - 1, 0)
        where id = p_photo_id;
        current_liked := false;
    else
        insert into public.photo_likes (photo_id, visitor_key)
        values (p_photo_id, p_visitor_key)
        on conflict do nothing;
        get diagnostics inserted_count = row_count;
        current_liked := inserted_count > 0;

        if inserted_count > 0 then
            update public.photos
            set likes_count = likes_count + 1
            where id = p_photo_id;
            insert into public.usage_minutes (bucket_start, photo_like_count)
            values (date_trunc('minute', now()), 1)
            on conflict (bucket_start) do update set
                photo_like_count = public.usage_minutes.photo_like_count + 1;
        end if;
    end if;

    select p.likes_count into current_count
    from public.photos p
    where p.id = p_photo_id;
    return jsonb_build_object('likes', current_count, 'liked', current_liked);
end;
$$;

create or replace function public.like_comment(p_comment_id integer, p_visitor_key text)
returns jsonb
language plpgsql
security definer
set search_path = pg_catalog, public, pg_temp
as $$
declare
    current_count integer;
    deleted_count integer;
    inserted_count integer;
    current_liked boolean;
begin
    if char_length(p_visitor_key) <> 64 then
        raise exception 'invalid visitor key' using errcode = '22023';
    end if;

    perform 1
    from public.comments c
    where c.id = p_comment_id
    for update;
    if not found then
        raise exception 'comment not found' using errcode = 'P0002';
    end if;

    delete from public.comment_likes
    where comment_id = p_comment_id and visitor_key = p_visitor_key;
    get diagnostics deleted_count = row_count;

    if deleted_count > 0 then
        update public.comments
        set likes_count = greatest(likes_count - 1, 0)
        where id = p_comment_id;
        current_liked := false;
    else
        insert into public.comment_likes (comment_id, visitor_key)
        values (p_comment_id, p_visitor_key)
        on conflict do nothing;
        get diagnostics inserted_count = row_count;
        current_liked := inserted_count > 0;

        if inserted_count > 0 then
            update public.comments
            set likes_count = likes_count + 1
            where id = p_comment_id;
            insert into public.usage_minutes (bucket_start, comment_like_count)
            values (date_trunc('minute', now()), 1)
            on conflict (bucket_start) do update set
                comment_like_count = public.usage_minutes.comment_like_count + 1;
        end if;
    end if;

    select c.likes_count into current_count
    from public.comments c
    where c.id = p_comment_id;
    return jsonb_build_object('likes', current_count, 'liked', current_liked);
end;
$$;

revoke all on function public.get_comments(integer, text) from public, anon, authenticated;
revoke all on function public.like_photo(integer, text) from public, anon, authenticated;
revoke all on function public.like_comment(integer, text) from public, anon, authenticated;
grant execute on function public.get_comments(integer, text) to service_role;
grant execute on function public.like_photo(integer, text) to service_role;
grant execute on function public.like_comment(integer, text) to service_role;
