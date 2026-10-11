CREATE TABLE public.orgs (
  id UUID PRIMARY KEY,
  name TEXT NOT NULL
);

CREATE TABLE public.users (
  id BIGSERIAL PRIMARY KEY,
  org_id UUID REFERENCES public.orgs(id),
  email TEXT NOT NULL
);

CREATE VIEW public.active_users AS
SELECT u.id, u.email
FROM public.users u
JOIN public.orgs o ON o.id = u.org_id;

CREATE FUNCTION public.touch_updated_at() RETURNS trigger AS $$
BEGIN
  UPDATE public.users SET email = email WHERE id = NEW.id;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_touch BEFORE UPDATE ON public.users
FOR EACH ROW EXECUTE FUNCTION public.touch_updated_at();

CREATE INDEX idx_users_org_id ON public.users (org_id);
