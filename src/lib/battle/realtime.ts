import { EventEmitter } from "node:events";

const battleEvents = new EventEmitter();
battleEvents.setMaxListeners(0);

function roomEventName(roomCode: string) {
  return `room:${roomCode}`;
}

function ticketEventName(ticketId: string) {
  return `ticket:${ticketId}`;
}

export function publishRoomEvent(roomCode: string) {
  battleEvents.emit(roomEventName(roomCode));
}

export function subscribeRoomEvent(roomCode: string, listener: () => void) {
  const name = roomEventName(roomCode);
  battleEvents.on(name, listener);
  return () => {
    battleEvents.off(name, listener);
  };
}

export function publishTicketEvent(ticketId: string) {
  battleEvents.emit(ticketEventName(ticketId));
}

export function subscribeTicketEvent(ticketId: string, listener: () => void) {
  const name = ticketEventName(ticketId);
  battleEvents.on(name, listener);
  return () => {
    battleEvents.off(name, listener);
  };
}
