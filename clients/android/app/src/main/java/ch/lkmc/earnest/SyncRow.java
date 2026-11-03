package ch.lkmc.earnest;

import androidx.annotation.NonNull;
import androidx.room.Entity;
import androidx.room.PrimaryKey;

@Entity
public class SyncRow {
    @PrimaryKey public int id;
    @NonNull public String json = "";
}
