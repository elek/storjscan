FROM archlinux
RUN pacman -Syu --noconfirm
RUN pacman -S --noconfirm go
