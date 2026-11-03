-- Agendamento real (story 3.5). A nota guarda de que agendamento e de que mês
-- (AAAA-MM) ela saiu; o índice único é o que impede duas notas no mesmo mês
-- mesmo com dois processos rodando o agendador ao mesmo tempo.
ALTER TABLE invoices ADD COLUMN schedule_id INTEGER NOT NULL DEFAULT 0;
ALTER TABLE invoices ADD COLUMN schedule_period TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX invoices_schedule_period ON invoices (schedule_id, schedule_period) WHERE schedule_id > 0;

-- production: o dono aceitou, na criação, emitir com valor fiscal por este
-- agendamento. notified_period: último mês cujo aviso de véspera saiu.
ALTER TABLE schedules ADD COLUMN production INTEGER NOT NULL DEFAULT 0;
ALTER TABLE schedules ADD COLUMN notified_period TEXT NOT NULL DEFAULT '';
