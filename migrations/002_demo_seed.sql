BEGIN;

INSERT INTO app_users (id, team_id, role, name)
VALUES
    ('atlas-intern', 'atlas', 'intern', 'Стажёр Atlas'),
    ('orbit-intern', 'orbit', 'intern', 'Стажёр Orbit')
ON CONFLICT (id) DO NOTHING;

INSERT INTO app_resources (team_id, tool_id, resource_id)
VALUES
    ('atlas', 'gitlab', 'atlas-training-repo'),
    ('orbit', 'gitlab', 'orbit-training-repo')
ON CONFLICT (team_id, tool_id, resource_id) DO NOTHING;

COMMIT;
